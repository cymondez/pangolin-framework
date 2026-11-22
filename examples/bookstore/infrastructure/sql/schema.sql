-- M1 tenant-only fixture schema. Deployment integration is completed in M3.
CREATE TABLE pangolin.books (
    tenant_id text NOT NULL,
    id text NOT NULL,
    title text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    version bigint NOT NULL DEFAULT 1,
    PRIMARY KEY (tenant_id, id)
);
CREATE TABLE pangolin.book_views (
    tenant_id text NOT NULL,
    id text NOT NULL,
    title text NOT NULL,
    version bigint NOT NULL,
    PRIMARY KEY (tenant_id, id)
);
CREATE TABLE pangolin.outbox (
    event_id text PRIMARY KEY,
    tenant_id text NOT NULL,
    envelope bytea NOT NULL,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','sent','dead')),
    lease_token text,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE pangolin.inbox (
    consumer_group text NOT NULL,
    event_id text NOT NULL,
    tenant_id text NOT NULL,
    envelope bytea NOT NULL,
    processed boolean NOT NULL DEFAULT false,
    PRIMARY KEY (consumer_group,event_id)
);
ALTER TABLE pangolin.books ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin.books FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_books ON pangolin.books USING (tenant_id = current_setting('pangolin.tenant_id', true)) WITH CHECK (tenant_id = current_setting('pangolin.tenant_id', true));
ALTER TABLE pangolin.book_views ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin.book_views FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_views ON pangolin.book_views USING (tenant_id = current_setting('pangolin.tenant_id', true)) WITH CHECK (tenant_id = current_setting('pangolin.tenant_id', true));
ALTER TABLE pangolin.outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin.outbox FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_outbox ON pangolin.outbox USING (tenant_id = current_setting('pangolin.tenant_id', true)) WITH CHECK (tenant_id = current_setting('pangolin.tenant_id', true));
CREATE POLICY worker_outbox ON pangolin.outbox TO pangolin_worker USING (true) WITH CHECK (true);
ALTER TABLE pangolin.inbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin.inbox FORCE ROW LEVEL SECURITY;
CREATE POLICY worker_inbox ON pangolin.inbox TO pangolin_worker USING (true) WITH CHECK (true);
GRANT SELECT, INSERT ON pangolin.books TO pangolin_app;
GRANT SELECT ON pangolin.book_views TO pangolin_app;
GRANT INSERT ON pangolin.outbox TO pangolin_app;
GRANT SELECT, UPDATE ON pangolin.outbox TO pangolin_worker;
GRANT SELECT, INSERT, UPDATE ON pangolin.inbox, pangolin.book_views TO pangolin_worker;
