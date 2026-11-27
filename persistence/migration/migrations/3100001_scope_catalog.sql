-- +goose Up
CREATE SCHEMA IF NOT EXISTS pangolin;
GRANT USAGE ON SCHEMA pangolin TO pangolin_app,pangolin_worker;
CREATE TABLE IF NOT EXISTS pangolin.resource_scopes(
 tenant_id text NOT NULL,scope_kind text NOT NULL,scope_id text NOT NULL,
 state text NOT NULL DEFAULT 'active',version bigint NOT NULL DEFAULT 1,
 PRIMARY KEY(tenant_id,scope_kind,scope_id),CHECK(scope_kind<>'tenant' OR scope_id=tenant_id)
);
GRANT SELECT,INSERT,UPDATE,REFERENCES ON pangolin.resource_scopes TO pangolin_app;
GRANT SELECT,REFERENCES ON pangolin.resource_scopes TO pangolin_worker;
ALTER TABLE pangolin.resource_scopes ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin.resource_scopes FORCE ROW LEVEL SECURITY;
CREATE POLICY catalog_tenant ON pangolin.resource_scopes TO pangolin_app
USING(tenant_id=current_setting('app.tenant_id',true)) WITH CHECK(tenant_id=current_setting('app.tenant_id',true));
CREATE POLICY catalog_authority ON pangolin.resource_scopes FOR SELECT TO pangolin_worker USING(true);
-- The deployment owner may synchronize metadata; this grants no business bypass.
-- +goose StatementBegin
DO $$ BEGIN
 EXECUTE format('CREATE POLICY catalog_deployment ON pangolin.resource_scopes TO %I USING(true) WITH CHECK(true)',current_user);
END $$;
-- +goose StatementEnd
-- Catalog is neutral metadata; it confers neither membership nor content access.
-- +goose Down
SELECT 1;
