-- +goose Up
CREATE SCHEMA pangolin_idempotency;
CREATE TABLE pangolin_idempotency.results(
 tenant_id text NOT NULL,scope_kind text NOT NULL,scope_id text NOT NULL,principal_kind text NOT NULL,principal_id text NOT NULL,
 operation text NOT NULL,key text NOT NULL,fingerprint bytea NOT NULL,result bytea,completed boolean NOT NULL DEFAULT false,
 expires_at timestamptz NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(tenant_id,scope_kind,scope_id,principal_kind,principal_id,operation,key)
);
ALTER TABLE pangolin_idempotency.results ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin_idempotency.results FORCE ROW LEVEL SECURITY;
CREATE POLICY exact_owner ON pangolin_idempotency.results USING(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true)) WITH CHECK(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true));
GRANT USAGE ON SCHEMA pangolin_idempotency TO pangolin_app,pangolin_worker;
GRANT SELECT,INSERT,UPDATE ON pangolin_idempotency.results TO pangolin_app,pangolin_worker;
-- +goose Down
SELECT 1;
