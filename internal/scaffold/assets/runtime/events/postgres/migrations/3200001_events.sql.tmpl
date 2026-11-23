-- +goose Up
CREATE SCHEMA pangolin_events;
CREATE TABLE pangolin_events.outbox(
 event_id text PRIMARY KEY,tenant_id text NOT NULL,scope_kind text NOT NULL,scope_id text NOT NULL,envelope bytea NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN('pending','processing','sent','dead')),lease_token text,locked_by text,locked_until timestamptz,
 attempts integer NOT NULL DEFAULT 0,next_attempt_at timestamptz NOT NULL DEFAULT now(),last_error text,sent_at timestamptz,created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE pangolin_events.inbox(
 consumer_group text NOT NULL,event_id text NOT NULL,tenant_id text NOT NULL,scope_kind text NOT NULL,scope_id text NOT NULL,envelope bytea NOT NULL,
 fingerprint bytea NOT NULL,verified_producer text NOT NULL,state text NOT NULL DEFAULT 'pending' CHECK(state IN('pending','processed','dead')),
 attempts integer NOT NULL DEFAULT 0,next_attempt_at timestamptz NOT NULL DEFAULT now(),version bigint NOT NULL DEFAULT 1,processed_at timestamptz,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(consumer_group,event_id)
);
CREATE INDEX outbox_due ON pangolin_events.outbox(state,next_attempt_at,created_at);
CREATE INDEX inbox_due ON pangolin_events.inbox(consumer_group,state,next_attempt_at,created_at);
ALTER TABLE pangolin_events.outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin_events.outbox FORCE ROW LEVEL SECURITY;
ALTER TABLE pangolin_events.inbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin_events.inbox FORCE ROW LEVEL SECURITY;
CREATE POLICY outbox_owner ON pangolin_events.outbox TO pangolin_app USING(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true)) WITH CHECK(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true));
CREATE POLICY outbox_queue ON pangolin_events.outbox TO pangolin_worker USING(true) WITH CHECK(true);
CREATE POLICY inbox_receiver ON pangolin_events.inbox FOR INSERT TO pangolin_worker WITH CHECK(true);
CREATE POLICY inbox_queue_read ON pangolin_events.inbox FOR SELECT TO pangolin_worker USING(true);
CREATE POLICY inbox_process ON pangolin_events.inbox FOR UPDATE TO pangolin_worker USING(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true)) WITH CHECK(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true));
GRANT USAGE ON SCHEMA pangolin_events TO pangolin_app,pangolin_worker;
GRANT INSERT,SELECT ON pangolin_events.outbox TO pangolin_app;
GRANT SELECT,UPDATE ON pangolin_events.outbox TO pangolin_worker;
GRANT SELECT,INSERT,UPDATE ON pangolin_events.inbox TO pangolin_worker;
-- +goose Down
SELECT 1;
