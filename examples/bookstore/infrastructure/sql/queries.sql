-- name: InsertBook :exec
INSERT INTO pangolin.books(tenant_id,id,title,version) VALUES ($1,$2,$3,$4);

-- name: GetBook :one
SELECT id,title,version FROM pangolin.books WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL;

-- name: UpdateBook :execrows
UPDATE pangolin.books SET title=$3,version=version+1 WHERE tenant_id=$1 AND id=$2 AND version=$4 AND deleted_at IS NULL;

-- name: DeleteBook :execrows
UPDATE pangolin.books SET deleted_at=now(),version=version+1 WHERE tenant_id=$1 AND id=$2 AND version=$3 AND deleted_at IS NULL;

-- name: InsertOutbox :exec
INSERT INTO pangolin.outbox(event_id,tenant_id,envelope) VALUES ($1,$2,$3);
