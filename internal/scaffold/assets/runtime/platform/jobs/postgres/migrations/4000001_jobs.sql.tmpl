-- +goose Up
CREATE SCHEMA pangolin_jobs;
CREATE TABLE pangolin_jobs.jobs(
 id text PRIMARY KEY,tenant_id text NOT NULL,scope_kind text NOT NULL,scope_id text NOT NULL,
 document bytea NOT NULL,fingerprint bytea NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN('pending','processing','succeeded','cancelled','dead')),
 due_at timestamptz NOT NULL,deadline timestamptz NOT NULL,attempts integer NOT NULL DEFAULT 0,
 lease_token text,locked_by text,locked_until timestamptz,cancel_requested boolean NOT NULL DEFAULT false,
 last_error text,created_at timestamptz NOT NULL DEFAULT now(),finished_at timestamptz,
 FOREIGN KEY(tenant_id,scope_kind,scope_id) REFERENCES pangolin.resource_scopes(tenant_id,scope_kind,scope_id)
);
CREATE INDEX jobs_due ON pangolin_jobs.jobs(state,due_at);
CREATE INDEX jobs_claim ON pangolin_jobs.jobs(due_at,id) WHERE state IN('pending','processing');
ALTER TABLE pangolin_jobs.jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin_jobs.jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY jobs_owner ON pangolin_jobs.jobs TO pangolin_app USING(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true)) WITH CHECK(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true));
CREATE POLICY jobs_queue ON pangolin_jobs.jobs TO pangolin_worker USING(true) WITH CHECK(true);
-- +goose StatementBegin
DO $$ BEGIN
 EXECUTE format('CREATE POLICY jobs_deployment ON pangolin_jobs.jobs TO %I USING(true) WITH CHECK(true)',current_user);
END $$;
-- +goose StatementEnd
GRANT USAGE ON SCHEMA pangolin_jobs TO pangolin_app,pangolin_worker;
GRANT SELECT,INSERT,UPDATE ON pangolin_jobs.jobs TO pangolin_app;
GRANT SELECT,UPDATE,DELETE ON pangolin_jobs.jobs TO pangolin_worker;
-- A narrow admission port checks the global capacity without exposing other
-- owners' queue rows to the application role. Its lock lives until UOW completion.
-- +goose StatementBegin
CREATE FUNCTION pangolin_jobs.admit(capacity integer) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(4000001);
 RETURN capacity>0 AND (SELECT count(*) FROM pangolin_jobs.jobs)<capacity;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION pangolin_jobs.admit(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pangolin_jobs.admit(integer) TO pangolin_app,pangolin_worker;
-- +goose Down
SELECT 1;
