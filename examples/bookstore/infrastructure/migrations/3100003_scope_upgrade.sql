-- +goose Up
-- The deployment lock and Goose transaction enclose the complete policy switch.
-- Existing rows keep tenant ownership; no Organization is guessed.
INSERT INTO pangolin.resource_scopes(tenant_id,scope_kind,scope_id)
SELECT DISTINCT tenant_id,'tenant',tenant_id FROM pangolin.books ON CONFLICT DO NOTHING;
ALTER TABLE pangolin.books ADD COLUMN owner_scope_kind text;
ALTER TABLE pangolin.books ADD COLUMN owner_scope_id text;
UPDATE pangolin.books SET owner_scope_kind='tenant',owner_scope_id=tenant_id;
ALTER TABLE pangolin.books ALTER COLUMN owner_scope_kind SET NOT NULL;
ALTER TABLE pangolin.books ALTER COLUMN owner_scope_id SET NOT NULL;
ALTER TABLE pangolin.books ADD CONSTRAINT books_owner_fk FOREIGN KEY(tenant_id,owner_scope_kind,owner_scope_id) REFERENCES pangolin.resource_scopes(tenant_id,scope_kind,scope_id);
ALTER TABLE pangolin.books DROP CONSTRAINT books_pkey;
ALTER TABLE pangolin.books ADD PRIMARY KEY(tenant_id,owner_scope_kind,owner_scope_id,id);
-- +goose StatementBegin
DO $$ DECLARE policy_name text; BEGIN
 FOR policy_name IN SELECT policyname FROM pg_policies WHERE schemaname='pangolin' AND tablename='books' LOOP
  EXECUTE format('DROP POLICY %I ON pangolin.books',policy_name);
 END LOOP;
END $$;
-- +goose StatementEnd
CREATE POLICY exact_owner ON pangolin.books
USING(tenant_id=current_setting('app.tenant_id',true) AND owner_scope_kind=current_setting('app.scope_kind',true) AND owner_scope_id=current_setting('app.scope_id',true))
WITH CHECK(tenant_id=current_setting('app.tenant_id',true) AND owner_scope_kind=current_setting('app.scope_kind',true) AND owner_scope_id=current_setting('app.scope_id',true));
ALTER TABLE pangolin.books ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin.books FORCE ROW LEVEL SECURITY;
-- Switch to the existing scope-aware Repository's table at the same commit.
-- The old tenant-only API's compatibility probe then refuses to start.
ALTER TABLE pangolin.books RENAME TO scope_books;
-- +goose Down
SELECT 1;
