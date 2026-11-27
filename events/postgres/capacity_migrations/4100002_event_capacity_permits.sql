-- +goose Up
CREATE TABLE pangolin_events.queue_capacity(
 queue text PRIMARY KEY CHECK(queue IN('outbox','inbox')),
 capacity integer NOT NULL CHECK(capacity>0),
 updated_at timestamptz NOT NULL DEFAULT now());
ALTER TABLE pangolin_events.queue_capacity ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin_events.queue_capacity FORCE ROW LEVEL SECURITY;
-- +goose StatementBegin
DO $$ BEGIN
 EXECUTE format('CREATE POLICY capacity_deployment ON pangolin_events.queue_capacity TO %I USING(true) WITH CHECK(true)',current_user);
END $$;
-- +goose StatementEnd

CREATE TABLE pangolin_events.outbox_permits(
 slot integer PRIMARY KEY,
 state text NOT NULL DEFAULT 'free' CHECK(state IN('free','assigned')),
 event_id text,owner_tx bigint,assigned_at timestamptz,
 CHECK(state<>'assigned' OR event_id IS NOT NULL),
 CHECK(state<>'free' OR (event_id IS NULL AND owner_tx IS NULL AND assigned_at IS NULL)));
ALTER TABLE pangolin_events.outbox_permits ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin_events.outbox_permits FORCE ROW LEVEL SECURITY;
-- +goose StatementBegin
DO $$ BEGIN
 EXECUTE format('CREATE POLICY outbox_permits_deployment ON pangolin_events.outbox_permits TO %I USING(true) WITH CHECK(true)',current_user);
END $$;
-- +goose StatementEnd
CREATE INDEX outbox_permits_free ON pangolin_events.outbox_permits(slot) WHERE state='free';

CREATE TABLE pangolin_events.inbox_permits(
 slot integer PRIMARY KEY,
 state text NOT NULL DEFAULT 'free' CHECK(state IN('free','assigned')),
 consumer_group text,event_id text,owner_tx bigint,assigned_at timestamptz,
 CHECK(state<>'assigned' OR (consumer_group IS NOT NULL AND event_id IS NOT NULL)),
 CHECK(state<>'free' OR (consumer_group IS NULL AND event_id IS NULL AND owner_tx IS NULL AND assigned_at IS NULL)));
ALTER TABLE pangolin_events.inbox_permits ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin_events.inbox_permits FORCE ROW LEVEL SECURITY;
-- +goose StatementBegin
DO $$ BEGIN
 EXECUTE format('CREATE POLICY inbox_permits_deployment ON pangolin_events.inbox_permits TO %I USING(true) WITH CHECK(true)',current_user);
END $$;
-- +goose StatementEnd
CREATE INDEX inbox_permits_free ON pangolin_events.inbox_permits(slot) WHERE state='free';

ALTER TABLE pangolin_events.outbox ADD COLUMN permit_slot integer;
ALTER TABLE pangolin_events.inbox ADD COLUMN permit_slot integer;
CREATE UNIQUE INDEX outbox_permit_slot_unique ON pangolin_events.outbox(permit_slot) WHERE permit_slot IS NOT NULL;
CREATE UNIQUE INDEX inbox_permit_slot_unique ON pangolin_events.inbox(permit_slot) WHERE permit_slot IS NOT NULL;
CREATE INDEX outbox_permit_owner_tx ON pangolin_events.outbox_permits(owner_tx,slot) WHERE state='assigned';
CREATE INDEX inbox_permit_owner_tx ON pangolin_events.inbox_permits(owner_tx,slot) WHERE state='assigned';
ALTER TABLE pangolin_events.outbox ADD CONSTRAINT outbox_permit_slot_fk FOREIGN KEY(permit_slot) REFERENCES pangolin_events.outbox_permits(slot);
ALTER TABLE pangolin_events.inbox ADD CONSTRAINT inbox_permit_slot_fk FOREIGN KEY(permit_slot) REFERENCES pangolin_events.inbox_permits(slot);
REVOKE ALL ON pangolin_events.queue_capacity,pangolin_events.outbox_permits,pangolin_events.inbox_permits FROM PUBLIC,pangolin_app,pangolin_worker;
-- +goose StatementBegin
DO $$ BEGIN
 EXECUTE format('CREATE POLICY outbox_deployment ON pangolin_events.outbox FOR UPDATE TO %I USING(true) WITH CHECK(true)',current_user);
 EXECUTE format('CREATE POLICY inbox_deployment ON pangolin_events.inbox FOR UPDATE TO %I USING(true) WITH CHECK(true)',current_user);
END $$;
-- +goose StatementEnd

-- Caller capacity GUCs are verified against the deployment config in the same
-- statement; a missing config row fails closed for bounded callers, while an
-- unbounded caller on a provisioned queue is refused explicitly.
-- +goose StatementBegin
CREATE FUNCTION pangolin_events.check_capacity_config(p_queue text,p_requested integer) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pangolin_events AS $$
DECLARE v_deployed integer;
BEGIN
 IF p_queue IS NULL OR p_requested IS NULL OR p_requested<=0 THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_CONFIG_CONFLICT' USING ERRCODE='PC003';
 END IF;
 IF p_queue NOT IN('outbox','inbox') THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_CONFIG_CONFLICT' USING ERRCODE='PC003';
 END IF;
 SELECT capacity INTO v_deployed FROM queue_capacity WHERE queue=p_queue;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_UNPROVISIONED' USING ERRCODE='PC002';
 END IF;
 IF p_requested<>v_deployed THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_CONFIG_CONFLICT' USING ERRCODE='PC003';
 END IF;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION pangolin_events.claim_outbox_permit() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pangolin_events AS $$
DECLARE v_slot integer;v_setting text;v_requested integer;v_deployed integer;
BEGIN
 IF NEW.permit_slot IS NOT NULL THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_PERMIT_INVALID' USING ERRCODE='42501';
 END IF;
 v_setting:=NULLIF(current_setting('pangolin.capacity_outbox',true),'');
 IF v_setting IS NULL THEN
  SELECT capacity INTO v_deployed FROM queue_capacity WHERE queue='outbox';
  IF NOT FOUND THEN RETURN NEW; END IF; -- legacy unbounded profile, not provisioned
  RAISE EXCEPTION 'EVENT_CAPACITY_REQUIRED' USING ERRCODE='PC004';
 END IF;
 BEGIN v_requested:=v_setting::integer;
 EXCEPTION WHEN invalid_text_representation THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_CONFIG_CONFLICT' USING ERRCODE='PC003';
 END;
 PERFORM check_capacity_config('outbox',v_requested);
 SELECT slot INTO v_slot FROM outbox_permits WHERE state='free' ORDER BY slot FOR UPDATE SKIP LOCKED LIMIT 1;
 IF v_slot IS NULL THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_EXCEEDED' USING ERRCODE='PC001';
 END IF;
 UPDATE outbox_permits SET state='assigned',event_id=NEW.event_id,owner_tx=txid_current(),assigned_at=now()
  WHERE slot=v_slot AND state='free';
 IF NOT FOUND THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_EXCEEDED' USING ERRCODE='PC001';
 END IF;
 NEW.permit_slot:=v_slot;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION pangolin_events.claim_inbox_permit() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pangolin_events AS $$
DECLARE v_slot integer;v_setting text;v_requested integer;v_deployed integer;
BEGIN
 IF NEW.permit_slot IS NOT NULL THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_PERMIT_INVALID' USING ERRCODE='42501';
 END IF;
 v_setting:=NULLIF(current_setting('pangolin.capacity_inbox',true),'');
 IF v_setting IS NULL THEN
  SELECT capacity INTO v_deployed FROM queue_capacity WHERE queue='inbox';
  IF NOT FOUND THEN RETURN NEW; END IF;
  RAISE EXCEPTION 'EVENT_CAPACITY_REQUIRED' USING ERRCODE='PC004';
 END IF;
 BEGIN v_requested:=v_setting::integer;
 EXCEPTION WHEN invalid_text_representation THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_CONFIG_CONFLICT' USING ERRCODE='PC003';
 END;
 PERFORM check_capacity_config('inbox',v_requested);
 IF EXISTS(SELECT 1 FROM pangolin_events.inbox WHERE consumer_group=NEW.consumer_group AND event_id=NEW.event_id) THEN
  RETURN NEW;
 END IF;
 SELECT slot INTO v_slot FROM inbox_permits WHERE state='free' ORDER BY slot FOR UPDATE SKIP LOCKED LIMIT 1;
 IF v_slot IS NULL THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_EXCEEDED' USING ERRCODE='PC001';
 END IF;
 UPDATE inbox_permits SET state='assigned',consumer_group=NEW.consumer_group,event_id=NEW.event_id,owner_tx=txid_current(),assigned_at=now()
  WHERE slot=v_slot AND state='free';
 IF NOT FOUND THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_EXCEEDED' USING ERRCODE='PC001';
 END IF;
 NEW.permit_slot:=v_slot;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Statement-end reconciliation keeps claims and rows paired. It releases only
-- claims whose row never materialized (duplicate inserts resolved as no-ops);
-- slot pairing protects the existing row's permit from being released.
-- +goose StatementBegin
CREATE FUNCTION pangolin_events.reconcile_outbox_permits() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pangolin_events AS $$
BEGIN
 UPDATE outbox_permits SET state='free',event_id=NULL,owner_tx=NULL,assigned_at=NULL
  WHERE owner_tx=txid_current() AND state='assigned'
   AND NOT EXISTS(SELECT 1 FROM pangolin_events.outbox WHERE permit_slot=outbox_permits.slot);
 RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION pangolin_events.reconcile_inbox_permits() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pangolin_events AS $$
BEGIN
 UPDATE inbox_permits SET state='free',consumer_group=NULL,event_id=NULL,owner_tx=NULL,assigned_at=NULL
  WHERE owner_tx=txid_current() AND state='assigned'
   AND NOT EXISTS(SELECT 1 FROM pangolin_events.inbox WHERE permit_slot=inbox_permits.slot);
 RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION pangolin_events.release_outbox_permit() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pangolin_events AS $$
BEGIN
 IF OLD.permit_slot IS NOT NULL THEN
  UPDATE outbox_permits SET state='free',event_id=NULL,owner_tx=NULL,assigned_at=NULL
   WHERE slot=OLD.permit_slot AND state='assigned' AND event_id=OLD.event_id;
  IF NOT FOUND THEN RAISE EXCEPTION 'EVENT_CAPACITY_PERMIT_CORRUPT' USING ERRCODE='PC010'; END IF;
 END IF;
 RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION pangolin_events.release_inbox_permit() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pangolin_events AS $$
BEGIN
 IF OLD.permit_slot IS NOT NULL THEN
  UPDATE inbox_permits SET state='free',consumer_group=NULL,event_id=NULL,owner_tx=NULL,assigned_at=NULL
   WHERE slot=OLD.permit_slot AND state='assigned' AND consumer_group=OLD.consumer_group AND event_id=OLD.event_id;
  IF NOT FOUND THEN RAISE EXCEPTION 'EVENT_CAPACITY_PERMIT_CORRUPT' USING ERRCODE='PC010'; END IF;
 END IF;
 RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER outbox_permit_claim BEFORE INSERT ON pangolin_events.outbox
 FOR EACH ROW EXECUTE FUNCTION pangolin_events.claim_outbox_permit();
CREATE TRIGGER outbox_permit_reconcile AFTER INSERT ON pangolin_events.outbox
 FOR EACH STATEMENT EXECUTE FUNCTION pangolin_events.reconcile_outbox_permits();
CREATE TRIGGER outbox_permit_release BEFORE DELETE ON pangolin_events.outbox
 FOR EACH ROW EXECUTE FUNCTION pangolin_events.release_outbox_permit();
CREATE TRIGGER inbox_permit_claim BEFORE INSERT ON pangolin_events.inbox
 FOR EACH ROW EXECUTE FUNCTION pangolin_events.claim_inbox_permit();
CREATE TRIGGER inbox_permit_reconcile AFTER INSERT ON pangolin_events.inbox
 FOR EACH STATEMENT EXECUTE FUNCTION pangolin_events.reconcile_inbox_permits();
CREATE TRIGGER inbox_permit_release BEFORE DELETE ON pangolin_events.inbox
 FOR EACH ROW EXECUTE FUNCTION pangolin_events.release_inbox_permit();

-- Runtime roles cannot rewrite queue identity or the permit-to-row binding.
-- The migration owner remains able to backfill permit_slot transactionally.
-- +goose StatementBegin
CREATE FUNCTION pangolin_events.guard_outbox_identity() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pangolin_events AS $$
DECLARE v_owner text;
BEGIN
 SELECT pg_get_userbyid(relowner) INTO v_owner FROM pg_class WHERE oid='pangolin_events.outbox'::regclass;
 IF (NEW.event_id,NEW.tenant_id,NEW.scope_kind,NEW.scope_id,NEW.permit_slot)
    IS DISTINCT FROM (OLD.event_id,OLD.tenant_id,OLD.scope_kind,OLD.scope_id,OLD.permit_slot)
    AND session_user<>v_owner THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_PERMIT_INVALID' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END;
$$;
CREATE FUNCTION pangolin_events.guard_inbox_identity() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pangolin_events AS $$
DECLARE v_owner text;
BEGIN
 SELECT pg_get_userbyid(relowner) INTO v_owner FROM pg_class WHERE oid='pangolin_events.inbox'::regclass;
 IF (NEW.consumer_group,NEW.event_id,NEW.tenant_id,NEW.scope_kind,NEW.scope_id,NEW.permit_slot)
    IS DISTINCT FROM (OLD.consumer_group,OLD.event_id,OLD.tenant_id,OLD.scope_kind,OLD.scope_id,OLD.permit_slot)
    AND session_user<>v_owner THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_PERMIT_INVALID' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER outbox_identity_immutable BEFORE UPDATE ON pangolin_events.outbox
 FOR EACH ROW EXECUTE FUNCTION pangolin_events.guard_outbox_identity();
CREATE TRIGGER inbox_identity_immutable BEFORE UPDATE ON pangolin_events.inbox
 FOR EACH ROW EXECUTE FUNCTION pangolin_events.guard_inbox_identity();

-- Legacy admission is replaced by trigger-owned permit claims; old binaries
-- calling it fail closed instead of silently bypassing the permit pool.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION pangolin_events.admit(queue text,capacity integer,group_name text,event text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 RAISE EXCEPTION 'EVENT_CAPACITY_UPGRADE_REQUIRED' USING ERRCODE='PC005';
END;
$$;
-- +goose StatementEnd

-- Deployment-only provisioning: installs/adopts/resizes the bounded permit pool.
-- It takes queue and permit table locks so no concurrent insert/delete/claim
-- interleaves with the transactional backfill, validates target capacity against
-- live rows, and updates the config atomically.
-- +goose StatementBegin
CREATE FUNCTION pangolin_events.provision_queue_permits(p_queue text,p_capacity integer,p_action text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pangolin_events AS $$
DECLARE
 v_current integer;v_live bigint;v_adopt bigint;v_assigned bigint;v_max integer;
 v_needed integer;v_removed integer;v_row record;
BEGIN
 IF p_queue IS NULL OR p_queue NOT IN('outbox','inbox') OR p_capacity IS NULL OR p_capacity<=0 OR p_action IS NULL OR (p_action<>'install' AND p_action<>'resize') THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_PROVISION_INVALID' USING ERRCODE='PC006';
 END IF;
 SET LOCAL lock_timeout='5s';
 PERFORM pg_advisory_xact_lock(4000004);
 IF p_queue='outbox' THEN
  LOCK TABLE pangolin_events.outbox IN SHARE ROW EXCLUSIVE MODE;
  LOCK TABLE pangolin_events.outbox_permits IN SHARE ROW EXCLUSIVE MODE;
 ELSE
  LOCK TABLE pangolin_events.inbox IN SHARE ROW EXCLUSIVE MODE;
  LOCK TABLE pangolin_events.inbox_permits IN SHARE ROW EXCLUSIVE MODE;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM queue_capacity WHERE queue=p_queue) THEN
  SELECT count(*) INTO v_live FROM pangolin_events.outbox WHERE p_queue='outbox';
  v_live:=v_live+(SELECT count(*) FROM pangolin_events.inbox WHERE p_queue='inbox');
  IF p_capacity < v_live THEN
   RAISE EXCEPTION 'EVENT_CAPACITY_PROVISION_BELOW_ROWS' USING ERRCODE='PC009',DETAIL='live='||v_live;
  END IF;
  INSERT INTO queue_capacity(queue,capacity) VALUES(p_queue,p_capacity);
  IF p_queue='outbox' THEN
   INSERT INTO pangolin_events.outbox_permits(slot) SELECT generate_series(1,p_capacity);
  ELSE
   INSERT INTO pangolin_events.inbox_permits(slot) SELECT generate_series(1,p_capacity);
  END IF;
  v_adopt:=LEAST(v_live,p_capacity);
  IF p_queue='outbox' THEN
   FOR v_row IN SELECT event_id,row_number() OVER(ORDER BY created_at,event_id) rn FROM pangolin_events.outbox ORDER BY rn LOOP
    IF v_row.rn>v_adopt THEN EXIT; END IF;
    UPDATE pangolin_events.outbox SET permit_slot=v_row.rn WHERE event_id=v_row.event_id;
    UPDATE pangolin_events.outbox_permits SET state='assigned',event_id=v_row.event_id,owner_tx=NULL,assigned_at=now()
     WHERE slot=v_row.rn AND state='free';
   END LOOP;
  ELSE
   FOR v_row IN SELECT consumer_group,event_id,row_number() OVER(ORDER BY created_at,consumer_group,event_id) rn FROM pangolin_events.inbox ORDER BY rn LOOP
    IF v_row.rn>v_adopt THEN EXIT; END IF;
    UPDATE pangolin_events.inbox SET permit_slot=v_row.rn WHERE consumer_group=v_row.consumer_group AND event_id=v_row.event_id;
    UPDATE pangolin_events.inbox_permits SET state='assigned',consumer_group=v_row.consumer_group,event_id=v_row.event_id,owner_tx=NULL,assigned_at=now()
     WHERE slot=v_row.rn AND state='free';
   END LOOP;
  END IF;
  IF p_queue='outbox' THEN
   SELECT count(*) INTO v_assigned FROM pangolin_events.outbox_permits WHERE state='assigned';
   SELECT count(*) INTO v_needed FROM pangolin_events.outbox_permits;
  ELSE
   SELECT count(*) INTO v_assigned FROM pangolin_events.inbox_permits WHERE state='assigned';
   SELECT count(*) INTO v_needed FROM pangolin_events.inbox_permits;
  END IF;
  IF v_assigned<>v_live THEN
   RAISE EXCEPTION 'EVENT_CAPACITY_PERMIT_CORRUPT' USING ERRCODE='PC010';
  END IF;
  IF v_needed<>p_capacity THEN
   RAISE EXCEPTION 'EVENT_CAPACITY_PERMIT_CORRUPT' USING ERRCODE='PC010';
  END IF;
  RETURN;
 END IF;
 SELECT capacity INTO v_current FROM queue_capacity WHERE queue=p_queue;
 IF p_action='install' THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_PROVISION_INSTALLED' USING ERRCODE='PC008';
 END IF;
 IF p_queue='outbox' THEN
  SELECT count(*) INTO v_assigned FROM pangolin_events.outbox_permits WHERE state='assigned';
  SELECT count(*) INTO v_live FROM pangolin_events.outbox;
 ELSE
  SELECT count(*) INTO v_assigned FROM pangolin_events.inbox_permits WHERE state='assigned';
  SELECT count(*) INTO v_live FROM pangolin_events.inbox;
 END IF;
 IF p_capacity<v_live OR p_capacity<v_assigned THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_PROVISION_BELOW_ROWS' USING ERRCODE='PC009',DETAIL='live='||v_live||',assigned='||v_assigned;
 END IF;
 IF p_capacity>v_current THEN
  v_needed:=p_capacity-v_current;
  IF p_queue='outbox' THEN
   SELECT COALESCE(max(slot),0) INTO v_max FROM pangolin_events.outbox_permits;
   INSERT INTO pangolin_events.outbox_permits(slot) SELECT generate_series(v_max+1,v_max+v_needed);
  ELSE
   SELECT COALESCE(max(slot),0) INTO v_max FROM pangolin_events.inbox_permits;
   INSERT INTO pangolin_events.inbox_permits(slot) SELECT generate_series(v_max+1,v_max+v_needed);
  END IF;
 ELSE
  v_needed:=v_current-p_capacity;
  IF p_queue='outbox' THEN
   DELETE FROM pangolin_events.outbox_permits WHERE state='free' AND slot IN
    (SELECT slot FROM pangolin_events.outbox_permits WHERE state='free' ORDER BY slot DESC LIMIT v_needed);
  ELSE
   DELETE FROM pangolin_events.inbox_permits WHERE state='free' AND slot IN
    (SELECT slot FROM pangolin_events.inbox_permits WHERE state='free' ORDER BY slot DESC LIMIT v_needed);
  END IF;
  GET DIAGNOSTICS v_removed=ROW_COUNT;
  IF v_removed<>v_needed THEN
   RAISE EXCEPTION 'EVENT_CAPACITY_PROVISION_BELOW_ROWS' USING ERRCODE='PC009';
  END IF;
 END IF;
 UPDATE queue_capacity SET capacity=p_capacity,updated_at=now() WHERE queue=p_queue;
 IF p_queue='outbox' THEN
  SELECT count(*) INTO v_assigned FROM pangolin_events.outbox_permits WHERE state='assigned';
  SELECT count(*) INTO v_needed FROM pangolin_events.outbox_permits;
 ELSE
  SELECT count(*) INTO v_assigned FROM pangolin_events.inbox_permits WHERE state='assigned';
  SELECT count(*) INTO v_needed FROM pangolin_events.inbox_permits;
 END IF;
 IF v_assigned<>v_live THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_PERMIT_CORRUPT' USING ERRCODE='PC010';
 END IF;
 IF v_needed<>p_capacity THEN
  RAISE EXCEPTION 'EVENT_CAPACITY_PERMIT_CORRUPT' USING ERRCODE='PC010';
 END IF;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION pangolin_events.provision_queue_permits(text,integer,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pangolin_events.provision_queue_permits(text,integer,text) TO current_user;
GRANT EXECUTE ON FUNCTION pangolin_events.check_capacity_config(text,integer) TO pangolin_app,pangolin_worker;
-- +goose Down
SELECT 1;
