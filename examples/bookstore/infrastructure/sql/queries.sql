-- name: InsertBook :exec
INSERT INTO pangolin.books(tenant_id,id,title,version) VALUES ($1,$2,$3,$4);

-- name: GetBook :one
SELECT id,title,version FROM pangolin.books WHERE tenant_id=$1 AND id=$2;

-- name: InsertOutbox :exec
INSERT INTO pangolin.outbox(event_id,tenant_id,envelope) VALUES ($1,$2,$3);
