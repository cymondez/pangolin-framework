-- name: InsertScopeBook :exec
INSERT INTO pangolin.scope_books(tenant_id,owner_scope_kind,owner_scope_id,id,title,version) VALUES($1,$2,$3,$4,$5,$6);
-- name: GetScopeBook :one
SELECT id,title,version FROM pangolin.scope_books WHERE tenant_id=$1 AND owner_scope_kind=$2 AND owner_scope_id=$3 AND id=$4 AND deleted_at IS NULL;
-- name: UpdateScopeBook :execrows
UPDATE pangolin.scope_books SET title=$5,version=version+1 WHERE tenant_id=$1 AND owner_scope_kind=$2 AND owner_scope_id=$3 AND id=$4 AND version=$6 AND deleted_at IS NULL;
-- name: DeleteScopeBook :execrows
UPDATE pangolin.scope_books SET deleted_at=now(),version=version+1 WHERE tenant_id=$1 AND owner_scope_kind=$2 AND owner_scope_id=$3 AND id=$4 AND version=$5 AND deleted_at IS NULL;
-- name: InsertScopeOutbox :exec
INSERT INTO pangolin.scope_outbox(event_id,tenant_id,scope_kind,scope_id,envelope) VALUES($1,$2,$3,$4,$5);
