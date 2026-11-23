-- +goose Up
-- The optional organization authority owns this hook on the local Tenant projection.
-- Revocation is never blocked by last-owner protection. Ownerless is persistent:
-- later Tenant reinstatement does not restore the scope without audited Recover.
-- +goose StatementBegin
DO $$ DECLARE table_name text; BEGIN
 FOREACH table_name IN ARRAY ARRAY['enterprises','organizations','owners'] LOOP
  EXECUTE format('CREATE POLICY owner_qualification_sync ON pangolin_organization.%I TO %I USING(true) WITH CHECK(true)',table_name,current_user);
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION pangolin_organization.tenant_owner_qualification_changed() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE target record; next_state text; next_version bigint; valid_owner boolean;
BEGIN
 IF OLD.state <> 'active' OR NEW.state='active' THEN RETURN NEW; END IF;
 FOR target IN
  SELECT c.tenant_id,c.scope_kind,c.scope_id,c.state FROM pangolin.resource_scopes c
  JOIN pangolin_organization.owners own ON own.tenant_id=c.tenant_id AND own.scope_kind=c.scope_kind AND own.scope_id=c.scope_id
  WHERE own.tenant_id=NEW.tenant_id AND own.principal_kind=NEW.principal_kind AND own.principal_id=NEW.principal_id AND own.state='active'
  ORDER BY c.tenant_id,c.scope_kind,c.scope_id FOR UPDATE OF c
 LOOP
  SELECT EXISTS(SELECT 1 FROM pangolin_organization.owners own
   JOIN pangolin_security.tenant_memberships tm ON tm.tenant_id=own.tenant_id AND tm.principal_kind=own.principal_kind AND tm.principal_id=own.principal_id
   WHERE own.tenant_id=target.tenant_id AND own.scope_kind=target.scope_kind AND own.scope_id=target.scope_id AND own.state='active' AND tm.state='active') INTO valid_owner;
  next_state:=target.state;
  IF NOT valid_owner AND target.state<>'archived' THEN next_state:='ownerless'; END IF;
  UPDATE pangolin.resource_scopes SET state=next_state,version=version+1
   WHERE tenant_id=target.tenant_id AND scope_kind=target.scope_kind AND scope_id=target.scope_id RETURNING version INTO next_version;
  IF target.scope_kind='org.organization' THEN
   UPDATE pangolin_organization.organizations SET state=next_state,version=next_version WHERE tenant_id=target.tenant_id AND id=target.scope_id;
  ELSIF target.scope_kind='org.enterprise' THEN
   UPDATE pangolin_organization.enterprises SET state=next_state,version=next_version WHERE tenant_id=target.tenant_id AND id=target.scope_id;
  END IF;
 END LOOP;
 RETURN NEW;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION pangolin_organization.tenant_owner_qualification_changed() FROM PUBLIC;
DROP TRIGGER IF EXISTS organization_tenant_owner_qualification ON pangolin_security.tenant_memberships;
CREATE TRIGGER organization_tenant_owner_qualification AFTER UPDATE OF state ON pangolin_security.tenant_memberships
FOR EACH ROW EXECUTE FUNCTION pangolin_organization.tenant_owner_qualification_changed();
-- +goose Down
SELECT 1;
