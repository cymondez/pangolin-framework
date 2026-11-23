-- +goose Up
CREATE SCHEMA IF NOT EXISTS pangolin_security;
GRANT USAGE ON SCHEMA pangolin TO pangolin_app,pangolin_worker;
GRANT SELECT ON pangolin.goose_db_version TO pangolin_app,pangolin_worker;
-- +goose StatementBegin
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='pangolin_scope_authority') THEN
  CREATE ROLE pangolin_scope_authority NOLOGIN NOSUPERUSER NOBYPASSRLS;
 END IF;
END $$;
-- +goose StatementEnd
GRANT pangolin_scope_authority TO pangolin_worker;
CREATE TABLE pangolin_security.tenant_memberships (
 tenant_id text NOT NULL, principal_kind text NOT NULL CHECK(principal_kind IN ('user','service')),
 principal_id text NOT NULL, state text NOT NULL CHECK(state IN ('active','revoked')),
 revision bigint NOT NULL CHECK(revision>0),
 PRIMARY KEY(tenant_id,principal_kind,principal_id)
);
GRANT USAGE ON SCHEMA pangolin_security TO pangolin_app,pangolin_worker;
GRANT SELECT ON pangolin_security.tenant_memberships TO pangolin_app,pangolin_worker;

-- +goose Down
-- Forward repair is the deployment policy. Rollback is intentionally unavailable.
SELECT 1;
