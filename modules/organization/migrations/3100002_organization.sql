-- +goose Up
CREATE SCHEMA pangolin_organization;
CREATE TABLE pangolin_organization.enterprises(
 tenant_id text NOT NULL,id text NOT NULL,slug text NOT NULL,state text NOT NULL DEFAULT 'active',version bigint NOT NULL DEFAULT 1,
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,slug)
);
CREATE TABLE pangolin_organization.organizations(
 tenant_id text NOT NULL,id text NOT NULL,enterprise_id text,slug text NOT NULL,state text NOT NULL DEFAULT 'active',version bigint NOT NULL DEFAULT 1,
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,slug),FOREIGN KEY(tenant_id,enterprise_id) REFERENCES pangolin_organization.enterprises(tenant_id,id)
);
CREATE TABLE pangolin_organization.organization_memberships(
 tenant_id text NOT NULL,organization_id text NOT NULL,principal_kind text NOT NULL,principal_id text NOT NULL,
 role text NOT NULL CHECK(role IN('owner','member')),state text NOT NULL DEFAULT 'active',version bigint NOT NULL DEFAULT 1,
 PRIMARY KEY(tenant_id,organization_id,principal_kind,principal_id),FOREIGN KEY(tenant_id,organization_id) REFERENCES pangolin_organization.organizations(tenant_id,id),
 FOREIGN KEY(tenant_id,principal_kind,principal_id) REFERENCES pangolin_security.tenant_memberships(tenant_id,principal_kind,principal_id)
);
CREATE TABLE pangolin_organization.enterprise_roles(
 tenant_id text NOT NULL,enterprise_id text NOT NULL,principal_kind text NOT NULL,principal_id text NOT NULL,
 role text NOT NULL CHECK(role IN('owner','manager')),state text NOT NULL DEFAULT 'active',version bigint NOT NULL DEFAULT 1,
 PRIMARY KEY(tenant_id,enterprise_id,principal_kind,principal_id),FOREIGN KEY(tenant_id,enterprise_id) REFERENCES pangolin_organization.enterprises(tenant_id,id),
 FOREIGN KEY(tenant_id,principal_kind,principal_id) REFERENCES pangolin_security.tenant_memberships(tenant_id,principal_kind,principal_id)
);
CREATE TABLE pangolin_organization.owners(
 tenant_id text NOT NULL,scope_kind text NOT NULL,scope_id text NOT NULL,principal_kind text NOT NULL,principal_id text NOT NULL,state text NOT NULL DEFAULT 'active',
 PRIMARY KEY(tenant_id,scope_kind,scope_id,principal_kind,principal_id),FOREIGN KEY(tenant_id,scope_kind,scope_id) REFERENCES pangolin.resource_scopes(tenant_id,scope_kind,scope_id)
);
CREATE TABLE pangolin_organization.scope_grants(
 tenant_id text NOT NULL,scope_kind text NOT NULL,scope_id text NOT NULL,principal_kind text NOT NULL CHECK(principal_kind='service'),principal_id text NOT NULL,role text NOT NULL,state text NOT NULL DEFAULT 'active',version bigint NOT NULL DEFAULT 1,
 PRIMARY KEY(tenant_id,scope_kind,scope_id,principal_kind,principal_id),FOREIGN KEY(tenant_id,scope_kind,scope_id) REFERENCES pangolin.resource_scopes(tenant_id,scope_kind,scope_id)
);
CREATE TABLE pangolin_organization.restrictions(
 tenant_id text NOT NULL,scope_kind text NOT NULL,scope_id text NOT NULL,action text NOT NULL,
 PRIMARY KEY(tenant_id,scope_kind,scope_id,action),FOREIGN KEY(tenant_id,scope_kind,scope_id) REFERENCES pangolin.resource_scopes(tenant_id,scope_kind,scope_id)
);
-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['enterprises','organizations','organization_memberships','enterprise_roles','owners','scope_grants','restrictions'] LOOP
  EXECUTE format('ALTER TABLE pangolin_organization.%I ENABLE ROW LEVEL SECURITY',t);
  EXECUTE format('ALTER TABLE pangolin_organization.%I FORCE ROW LEVEL SECURITY',t);
  EXECUTE format('CREATE POLICY governance ON pangolin_organization.%I TO pangolin_app USING(tenant_id=current_setting(''app.tenant_id'',true)) WITH CHECK(tenant_id=current_setting(''app.tenant_id'',true))',t);
  EXECUTE format('CREATE POLICY authority_read ON pangolin_organization.%I FOR SELECT TO pangolin_scope_authority USING(true)',t);
 END LOOP;
END $$;
-- +goose StatementEnd
GRANT USAGE ON SCHEMA pangolin_organization TO pangolin_app,pangolin_scope_authority;
GRANT SELECT,INSERT,UPDATE ON ALL TABLES IN SCHEMA pangolin_organization TO pangolin_app;
GRANT SELECT ON ALL TABLES IN SCHEMA pangolin_organization TO pangolin_scope_authority;
-- +goose Down
SELECT 1;
