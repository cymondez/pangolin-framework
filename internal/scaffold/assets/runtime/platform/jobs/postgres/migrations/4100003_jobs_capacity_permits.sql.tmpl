-- +goose Up
CREATE TABLE pangolin_jobs.queue_capacity(
 queue text PRIMARY KEY CHECK(queue='jobs'),
 capacity integer NOT NULL CHECK(capacity>0),
 updated_at timestamptz NOT NULL DEFAULT now());
ALTER TABLE pangolin_jobs.queue_capacity ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin_jobs.queue_capacity FORCE ROW LEVEL SECURITY;
-- +goose StatementBegin
DO $$ BEGIN
 EXECUTE format('CREATE POLICY jobs_capacity_deployment ON pangolin_jobs.queue_capacity TO %I USING(true) WITH CHECK(true)',current_user);
END $$;
-- +goose StatementEnd

CREATE TABLE pangolin_jobs.jobs_permits(
 slot integer PRIMARY KEY CHECK(slot>0),
 state text NOT NULL DEFAULT 'free' CHECK(state IN('free','assigned')),
 job_id text,owner_tx bigint,assigned_at timestamptz,
 CHECK(state<>'assigned' OR job_id IS NOT NULL),
 CHECK(state<>'free' OR (job_id IS NULL AND owner_tx IS NULL AND assigned_at IS NULL)));
ALTER TABLE pangolin_jobs.jobs_permits ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin_jobs.jobs_permits FORCE ROW LEVEL SECURITY;
-- +goose StatementBegin
DO $$ BEGIN
 EXECUTE format('CREATE POLICY jobs_permits_deployment ON pangolin_jobs.jobs_permits TO %I USING(true) WITH CHECK(true)',current_user);
END $$;
-- +goose StatementEnd
CREATE INDEX jobs_permits_free ON pangolin_jobs.jobs_permits(slot) WHERE state='free';
CREATE INDEX jobs_permits_assigned_owner_tx ON pangolin_jobs.jobs_permits(owner_tx)
 WHERE state='assigned' AND owner_tx IS NOT NULL;

ALTER TABLE pangolin_jobs.jobs ADD COLUMN permit_slot integer;
ALTER TABLE pangolin_jobs.jobs ADD CONSTRAINT jobs_permit_slot_fk
 FOREIGN KEY(permit_slot) REFERENCES pangolin_jobs.jobs_permits(slot);
CREATE UNIQUE INDEX jobs_permit_slot_unique ON pangolin_jobs.jobs(permit_slot) WHERE permit_slot IS NOT NULL;
-- jobs_deployment policy (4000001) already grants the deployment role row access.

-- Runtime enqueue supplies its configured bound in both the transaction-local
-- GUC (for the trigger) and this independent comparison (for the app policy).
-- This helper deliberately does not read the GUC: SQL expression evaluation
-- order within the caller's CTE is unspecified.
-- +goose StatementBegin
CREATE FUNCTION pangolin_jobs.check_capacity_config(p_requested integer) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pangolin_jobs AS $$
DECLARE v_deployed integer;
BEGIN
 SELECT capacity INTO v_deployed FROM pangolin_jobs.queue_capacity WHERE queue='jobs';
 IF NOT FOUND THEN
  RAISE EXCEPTION 'JOB_CAPACITY_UNPROVISIONED' USING ERRCODE='JC002';
 END IF;
 IF p_requested IS DISTINCT FROM v_deployed THEN
  RAISE EXCEPTION 'JOB_CAPACITY_CONFIG_CONFLICT' USING ERRCODE='JC003';
 END IF;
END;
$$;
-- +goose StatementEnd

-- The SECURITY INVOKER guard permits binding changes only while executed as
-- the jobs table owner (the deployment identity used by provisioning). Runtime
-- identities cannot change a bound job ID or its permit slot.
-- +goose StatementBegin
CREATE FUNCTION pangolin_jobs.guard_job_permit_binding() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,pangolin_jobs AS $$
DECLARE v_owner name;
BEGIN
 IF OLD.id IS DISTINCT FROM NEW.id OR OLD.permit_slot IS DISTINCT FROM NEW.permit_slot THEN
  SELECT pg_get_userbyid(c.relowner) INTO v_owner
   FROM pg_class c WHERE c.oid='pangolin_jobs.jobs'::regclass;
  IF current_user <> v_owner THEN
   RAISE EXCEPTION 'JOB_PERMIT_BINDING_FORBIDDEN' USING ERRCODE='JC011';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- A present GUC is parsed and checked against the deployed bound here. An
-- absent/empty GUC is only the legacy deployment seed path when no capacity
-- configuration exists; once provisioned, every runtime insert fails closed.
-- The permit slot is always selected and assigned by this trigger. A supplied
-- NEW.permit_slot is never treated as a claim.
-- +goose StatementBegin
CREATE FUNCTION pangolin_jobs.claim_job_permit() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pangolin_jobs AS $$
DECLARE
 v_requested_text text;
 v_requested integer;
 v_deployed integer;
 v_configured boolean;
 v_slot integer;
BEGIN
 IF NEW.permit_slot IS NOT NULL THEN
  RAISE EXCEPTION 'JOB_PERMIT_BINDING_FORBIDDEN' USING ERRCODE='JC011';
 END IF;
 v_requested_text:=nullif(current_setting('pangolin.capacity_jobs',true),'');
 SELECT capacity INTO v_deployed FROM pangolin_jobs.queue_capacity WHERE queue='jobs';
 v_configured:=FOUND;
 IF v_requested_text IS NULL THEN
  IF NOT v_configured THEN
   RETURN NEW;
  END IF;
  RAISE EXCEPTION 'JOB_CAPACITY_REQUIRED' USING ERRCODE='JC004';
 END IF;
 BEGIN
  v_requested:=v_requested_text::integer;
 EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN
  RAISE EXCEPTION 'JOB_CAPACITY_INVALID' USING ERRCODE='JC012';
 END;
 IF NOT v_configured THEN
  RAISE EXCEPTION 'JOB_CAPACITY_UNPROVISIONED' USING ERRCODE='JC002';
 END IF;
 IF v_requested IS DISTINCT FROM v_deployed THEN
  RAISE EXCEPTION 'JOB_CAPACITY_CONFIG_CONFLICT' USING ERRCODE='JC003';
 END IF;
 SELECT slot INTO v_slot FROM pangolin_jobs.jobs_permits
  WHERE state='free' ORDER BY slot FOR UPDATE SKIP LOCKED LIMIT 1;
 IF v_slot IS NULL THEN
  RAISE EXCEPTION 'JOB_CAPACITY_EXCEEDED' USING ERRCODE='JC001';
 END IF;
 UPDATE pangolin_jobs.jobs_permits
  SET state='assigned',job_id=NEW.id,owner_tx=txid_current(),assigned_at=now()
  WHERE slot=v_slot AND state='free';
 IF NOT FOUND THEN
  RAISE EXCEPTION 'JOB_CAPACITY_EXCEEDED' USING ERRCODE='JC001';
 END IF;
 NEW.permit_slot:=v_slot;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Release only claims made by this transaction that have no exact job/slot
-- binding. The owner_tx partial index and unique permit_slot index bound the
-- reconciliation work; this is not a queue-wide scan on the hot path.
-- +goose StatementBegin
CREATE FUNCTION pangolin_jobs.reconcile_job_permits() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pangolin_jobs AS $$
BEGIN
 UPDATE pangolin_jobs.jobs_permits p
  SET state='free',job_id=NULL,owner_tx=NULL,assigned_at=NULL
  WHERE p.owner_tx=txid_current() AND p.state='assigned'
   AND NOT EXISTS(
    SELECT 1 FROM pangolin_jobs.jobs j
    WHERE j.permit_slot=p.slot AND j.id=p.job_id);
 RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- Deleting a job releases exactly the permit that was bound to that job. A
-- stale/mismatched binding aborts the delete instead of freeing another job's
-- capacity.
-- +goose StatementBegin
CREATE FUNCTION pangolin_jobs.release_job_permit() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pangolin_jobs AS $$
DECLARE v_released integer;
BEGIN
 IF OLD.permit_slot IS NOT NULL THEN
  UPDATE pangolin_jobs.jobs_permits
   SET state='free',job_id=NULL,owner_tx=NULL,assigned_at=NULL
   WHERE slot=OLD.permit_slot AND job_id=OLD.id AND state='assigned';
  GET DIAGNOSTICS v_released=ROW_COUNT;
  IF v_released<>1 THEN
   RAISE EXCEPTION 'JOB_PERMIT_BINDING_CORRUPT' USING ERRCODE='JC010';
  END IF;
 END IF;
 RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER jobs_permit_binding_guard BEFORE UPDATE ON pangolin_jobs.jobs
 FOR EACH ROW EXECUTE FUNCTION pangolin_jobs.guard_job_permit_binding();
CREATE TRIGGER jobs_permit_claim BEFORE INSERT ON pangolin_jobs.jobs
 FOR EACH ROW EXECUTE FUNCTION pangolin_jobs.claim_job_permit();
CREATE TRIGGER jobs_permit_reconcile AFTER INSERT ON pangolin_jobs.jobs
 FOR EACH STATEMENT EXECUTE FUNCTION pangolin_jobs.reconcile_job_permits();
CREATE TRIGGER jobs_permit_release BEFORE DELETE ON pangolin_jobs.jobs
 FOR EACH ROW EXECUTE FUNCTION pangolin_jobs.release_job_permit();

-- Restrict application inserts and all runtime updates to non-binding columns.
-- The trigger guard remains defense in depth against inherited privileges.
REVOKE INSERT,UPDATE ON pangolin_jobs.jobs FROM pangolin_app;
REVOKE UPDATE ON pangolin_jobs.jobs FROM pangolin_worker;
GRANT INSERT(id,tenant_id,scope_kind,scope_id,document,fingerprint,due_at,deadline)
 ON pangolin_jobs.jobs TO pangolin_app;
GRANT UPDATE(state,due_at,attempts,cancel_requested,last_error,finished_at)
 ON pangolin_jobs.jobs TO pangolin_app;
GRANT UPDATE(state,due_at,attempts,lease_token,locked_by,locked_until,cancel_requested,last_error,finished_at)
 ON pangolin_jobs.jobs TO pangolin_worker;

-- Legacy admission is replaced by trigger-owned permit claims; old binaries
-- calling it fail closed instead of silently bypassing the permit pool.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION pangolin_jobs.admit(capacity integer) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 RAISE EXCEPTION 'JOB_CAPACITY_UPGRADE_REQUIRED' USING ERRCODE='JC005';
END;
$$;
-- +goose StatementEnd

-- Deployment-only provisioning serializes with normal queue writes by locking
-- the jobs table before permits. The advisory lock is limited to deployment
-- changes and is never acquired by the enqueue hot path.
-- +goose StatementBegin
CREATE FUNCTION pangolin_jobs.provision_permits(p_capacity integer,p_action text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pangolin_jobs AS $$
DECLARE
 v_current integer;
 v_live bigint;
 v_assigned bigint;
 v_max integer;
 v_needed integer;
 v_removed integer;
 v_actual integer;
 v_assigned_after integer;
 v_row record;
BEGIN
 IF p_capacity IS NULL OR p_capacity<=0 OR p_action IS NULL OR p_action NOT IN('install','resize') THEN
  RAISE EXCEPTION 'JOB_CAPACITY_PROVISION_INVALID' USING ERRCODE='JC006';
 END IF;
 SET LOCAL lock_timeout='5s';
 PERFORM pg_advisory_xact_lock(4000005);
 LOCK TABLE pangolin_jobs.jobs IN SHARE ROW EXCLUSIVE MODE;
 LOCK TABLE pangolin_jobs.jobs_permits IN SHARE ROW EXCLUSIVE MODE;
 SELECT count(*) INTO v_live FROM pangolin_jobs.jobs;
 IF NOT EXISTS(SELECT 1 FROM pangolin_jobs.queue_capacity WHERE queue='jobs') THEN
  IF p_capacity<v_live THEN
   RAISE EXCEPTION 'JOB_CAPACITY_PROVISION_BELOW_ROWS' USING ERRCODE='JC009',DETAIL='live='||v_live;
  END IF;
  INSERT INTO pangolin_jobs.queue_capacity(queue,capacity) VALUES('jobs',p_capacity);
  INSERT INTO pangolin_jobs.jobs_permits(slot) SELECT generate_series(1,p_capacity);
  FOR v_row IN
   SELECT id,row_number() OVER(ORDER BY created_at,id) AS rn
   FROM pangolin_jobs.jobs ORDER BY created_at,id
  LOOP
   UPDATE pangolin_jobs.jobs SET permit_slot=v_row.rn WHERE id=v_row.id;
   UPDATE pangolin_jobs.jobs_permits
    SET state='assigned',job_id=v_row.id,owner_tx=NULL,assigned_at=now()
    WHERE slot=v_row.rn AND state='free';
   IF NOT FOUND THEN
    RAISE EXCEPTION 'JOB_PERMIT_BINDING_CORRUPT' USING ERRCODE='JC010';
   END IF;
  END LOOP;
  SELECT count(*) INTO v_actual FROM pangolin_jobs.jobs_permits;
  SELECT count(*) INTO v_assigned_after FROM pangolin_jobs.jobs_permits WHERE state='assigned';
  IF v_actual<>p_capacity OR v_assigned_after<>v_live OR EXISTS(
   SELECT 1 FROM pangolin_jobs.jobs j
   LEFT JOIN pangolin_jobs.jobs_permits p
    ON p.slot=j.permit_slot AND p.job_id=j.id AND p.state='assigned'
   WHERE p.slot IS NULL) THEN
   RAISE EXCEPTION 'JOB_PERMIT_BINDING_CORRUPT' USING ERRCODE='JC010';
  END IF;
  RETURN;
 END IF;

 SELECT capacity INTO v_current FROM pangolin_jobs.queue_capacity WHERE queue='jobs' FOR UPDATE;
 IF p_action='install' THEN
  RAISE EXCEPTION 'JOB_CAPACITY_PROVISION_INSTALLED' USING ERRCODE='JC008';
 END IF;
 SELECT count(*) INTO v_assigned FROM pangolin_jobs.jobs_permits WHERE state='assigned';
 IF p_capacity<v_live OR p_capacity<v_assigned THEN
  RAISE EXCEPTION 'JOB_CAPACITY_PROVISION_BELOW_ROWS' USING ERRCODE='JC009',DETAIL='live='||v_live||',assigned='||v_assigned;
 END IF;
 IF p_capacity>v_current THEN
  SELECT COALESCE(max(slot),0) INTO v_max FROM pangolin_jobs.jobs_permits;
  IF p_capacity::bigint-v_current::bigint > 2147483647::bigint-v_max::bigint THEN
   RAISE EXCEPTION 'JOB_CAPACITY_PROVISION_INVALID' USING ERRCODE='JC006';
  END IF;
  INSERT INTO pangolin_jobs.jobs_permits(slot)
   SELECT generate_series(v_max+1,v_max+(p_capacity-v_current));
 ELSIF p_capacity<v_current THEN
  v_needed:=v_current-p_capacity;
  DELETE FROM pangolin_jobs.jobs_permits
   WHERE slot IN (
    SELECT slot FROM pangolin_jobs.jobs_permits
    WHERE state='free' ORDER BY slot DESC LIMIT v_needed);
  GET DIAGNOSTICS v_removed=ROW_COUNT;
  IF v_removed<>v_needed THEN
   RAISE EXCEPTION 'JOB_CAPACITY_PROVISION_BELOW_ROWS' USING ERRCODE='JC009';
  END IF;
 END IF;
 UPDATE pangolin_jobs.queue_capacity SET capacity=p_capacity,updated_at=now() WHERE queue='jobs';
 SELECT count(*) INTO v_assigned FROM pangolin_jobs.jobs_permits WHERE state='assigned';
 SELECT count(*) INTO v_actual FROM pangolin_jobs.jobs_permits;
 IF v_assigned<>v_live OR v_actual<>p_capacity OR EXISTS(
  SELECT 1 FROM pangolin_jobs.jobs j
  LEFT JOIN pangolin_jobs.jobs_permits p
   ON p.slot=j.permit_slot AND p.job_id=j.id AND p.state='assigned'
  WHERE p.slot IS NULL) THEN
  RAISE EXCEPTION 'JOB_PERMIT_BINDING_CORRUPT' USING ERRCODE='JC010';
 END IF;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION pangolin_jobs.provision_permits(integer,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pangolin_jobs.provision_permits(integer,text) TO current_user;
REVOKE ALL ON FUNCTION pangolin_jobs.check_capacity_config(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pangolin_jobs.check_capacity_config(integer) TO pangolin_app,pangolin_worker;
-- +goose Down
SELECT 1;
