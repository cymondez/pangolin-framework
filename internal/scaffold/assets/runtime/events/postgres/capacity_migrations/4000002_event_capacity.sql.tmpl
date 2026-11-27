-- +goose Up
-- +goose StatementBegin
DO $$ BEGIN
 EXECUTE format('CREATE POLICY outbox_admission ON pangolin_events.outbox FOR SELECT TO %I USING(true)',current_user);
 EXECUTE format('CREATE POLICY inbox_admission ON pangolin_events.inbox FOR SELECT TO %I USING(true)',current_user);
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION pangolin_events.admit(queue text,capacity integer,group_name text,event text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF capacity<=0 THEN RETURN false; END IF;
 IF queue='outbox' THEN
  PERFORM pg_advisory_xact_lock(4000002);
  RETURN (SELECT count(*) FROM pangolin_events.outbox)<capacity;
 ELSIF queue='inbox' THEN
  PERFORM pg_advisory_xact_lock(4000003);
  RETURN EXISTS(SELECT 1 FROM pangolin_events.inbox WHERE consumer_group=group_name AND event_id=event)
      OR (SELECT count(*) FROM pangolin_events.inbox)<capacity;
 END IF;
 RETURN false;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION pangolin_events.admit(text,integer,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pangolin_events.admit(text,integer,text,text) TO pangolin_app,pangolin_worker;
-- +goose Down
SELECT 1;
