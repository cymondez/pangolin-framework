-- Disposable local test credentials. No framework business tables are created here.
CREATE ROLE pangolin_owner NOLOGIN NOSUPERUSER NOBYPASSRLS;
CREATE ROLE pangolin_migrator LOGIN PASSWORD 'local-migration-only' NOINHERIT NOSUPERUSER NOBYPASSRLS;
GRANT pangolin_owner TO pangolin_migrator;
CREATE ROLE pangolin_app LOGIN PASSWORD 'local-app-only' NOSUPERUSER NOBYPASSRLS;
CREATE ROLE pangolin_worker LOGIN PASSWORD 'local-worker-only' NOSUPERUSER NOBYPASSRLS;
GRANT CONNECT ON DATABASE pangolin_m0 TO pangolin_migrator, pangolin_app, pangolin_worker;
CREATE SCHEMA pangolin AUTHORIZATION pangolin_owner;
GRANT USAGE ON SCHEMA pangolin TO pangolin_app, pangolin_worker;
-- Table grants, RLS policies and queue-specific privileges are migration-owned.
