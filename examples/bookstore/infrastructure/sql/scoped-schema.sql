-- Optional organization-enabled Bookstore fixture; never installed tenant-only.
CREATE TABLE pangolin.resource_scopes (
 tenant_id text NOT NULL,scope_kind text NOT NULL,scope_id text NOT NULL,
 state text NOT NULL DEFAULT 'active',version bigint NOT NULL DEFAULT 1,
 PRIMARY KEY(tenant_id,scope_kind,scope_id)
);
CREATE TABLE pangolin.scope_books (
 tenant_id text NOT NULL,owner_scope_kind text NOT NULL,owner_scope_id text NOT NULL,
 id text NOT NULL,title text NOT NULL,version bigint NOT NULL DEFAULT 1,deleted_at timestamptz,
 PRIMARY KEY(tenant_id,owner_scope_kind,owner_scope_id,id),
 FOREIGN KEY(tenant_id,owner_scope_kind,owner_scope_id) REFERENCES pangolin.resource_scopes(tenant_id,scope_kind,scope_id)
);
CREATE TABLE pangolin.scope_outbox (
 event_id text PRIMARY KEY,tenant_id text NOT NULL,scope_kind text NOT NULL,scope_id text NOT NULL,envelope bytea NOT NULL,
 FOREIGN KEY(tenant_id,scope_kind,scope_id) REFERENCES pangolin.resource_scopes(tenant_id,scope_kind,scope_id)
);
ALTER TABLE pangolin.scope_books ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin.scope_books FORCE ROW LEVEL SECURITY;
CREATE POLICY scope_books ON pangolin.scope_books
USING(tenant_id=current_setting('app.tenant_id',true) AND owner_scope_kind=current_setting('app.scope_kind',true) AND owner_scope_id=current_setting('app.scope_id',true))
WITH CHECK(tenant_id=current_setting('app.tenant_id',true) AND owner_scope_kind=current_setting('app.scope_kind',true) AND owner_scope_id=current_setting('app.scope_id',true));
ALTER TABLE pangolin.scope_outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin.scope_outbox FORCE ROW LEVEL SECURITY;
CREATE POLICY scope_outbox ON pangolin.scope_outbox
USING(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true))
WITH CHECK(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true));
GRANT SELECT,INSERT,UPDATE ON pangolin.scope_books TO pangolin_app;
GRANT SELECT,INSERT ON pangolin.scope_outbox TO pangolin_app;
GRANT REFERENCES ON pangolin.resource_scopes TO pangolin_app;
