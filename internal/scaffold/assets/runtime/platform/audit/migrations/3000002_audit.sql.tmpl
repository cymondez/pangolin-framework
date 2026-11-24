-- +goose Up
CREATE SCHEMA pangolin_audit;
CREATE TABLE pangolin_audit.records (
 id text PRIMARY KEY, tenant_id text NOT NULL, scope_kind text NOT NULL, scope_id text NOT NULL,
 principal_kind text NOT NULL, principal_id text NOT NULL, action text NOT NULL, resource text NOT NULL,
 result text NOT NULL, trace text NOT NULL,duration_ns bigint NOT NULL,at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE pangolin_audit.records ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin_audit.records FORCE ROW LEVEL SECURITY;
CREATE POLICY exact_owner ON pangolin_audit.records
USING(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true))
WITH CHECK(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true));
GRANT USAGE ON SCHEMA pangolin_audit TO pangolin_app,pangolin_worker;
GRANT SELECT,INSERT ON pangolin_audit.records TO pangolin_app,pangolin_worker;
-- +goose Down
SELECT 1;
