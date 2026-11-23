// Package idempotency reserves command keys and their success result in one UOW.
package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/jackc/pgx/v5"
)

var ErrConflict = errors.New("IDEMPOTENCY_CONFLICT")
var ErrInProgress = errors.New("IDEMPOTENCY_IN_PROGRESS")
var ErrExpired = errors.New("IDEMPOTENCY_EXPIRED")

type Config struct {
	MaxKeyBytes, MaxBodyBytes int
	RetryWindow, Retention    time.Duration
	Protect, Unprotect        func([]byte) ([]byte, error)
}
type Store struct {
	config    Config
	authorize func(context.Context, string) error
}

func New(c Config, authorize func(context.Context, string) error) (*Store, error) {
	if c.MaxKeyBytes <= 0 || c.MaxBodyBytes <= 0 || c.RetryWindow <= 0 || c.Retention < c.RetryWindow || c.Protect == nil || c.Unprotect == nil || authorize == nil {
		return nil, uow.ErrOptionsInvalid
	}
	return &Store{c, authorize}, nil
}

// CanonicalFingerprint hashes canonical JSON, including the caller's operation
// and current full owner in the DB unique key. Invalid JSON is rejected.
func CanonicalFingerprint(body []byte) ([]byte, error) {
	var value any
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, uow.ErrOptionsInvalid
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(raw)
	return hash[:], nil
}
func (s *Store) identity(ctx context.Context, operation, key string) (scope.ScopeBinding, error) {
	b, ok := scope.Current(ctx)
	if !ok {
		return b, scope.ErrDenied
	}
	if operation == "" || key == "" || len(key) > s.config.MaxKeyBytes {
		return b, uow.ErrOptionsInvalid
	}
	if err := s.authorize(ctx, operation); err != nil {
		return b, err
	}
	return b, nil
}

// Execute requires an existing write UOW. A pending key is never independently
// committed. work must return a safe replay result before the root completes.
func (s *Store) Execute(ctx context.Context, operation, key string, body []byte, work func(context.Context) ([]byte, error)) (resultValue []byte, replayed bool, returned error) {
	defer func() {
		if panicValue := recover(); panicValue != nil {
			_ = uow.Fail(ctx, errors.New("idempotency callback panicked"))
			panic(panicValue)
		}
		if returned != nil {
			returned = uow.Fail(ctx, returned)
		}
	}()
	if work == nil {
		return nil, false, uow.ErrOptionsInvalid
	}
	b, err := s.identity(ctx, operation, key)
	if err != nil {
		return nil, false, err
	}
	if len(body) > s.config.MaxBodyBytes {
		return nil, false, uow.ErrOptionsInvalid
	}
	hash, err := CanonicalFingerprint(body)
	if err != nil {
		return nil, false, err
	}
	ex, err := postgres.Current(ctx, true)
	if err != nil {
		return nil, false, err
	}
	args := []any{b.Ref.TenantID, b.Ref.Kind, b.Ref.ID, b.Principal.Kind, b.Principal.ID, operation, key}
	var inserted string
	err = ex.QueryRow(ctx, `INSERT INTO pangolin_idempotency.results(tenant_id,scope_kind,scope_id,principal_kind,principal_id,operation,key,fingerprint,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,now()+$9*interval '1 microsecond') ON CONFLICT DO NOTHING RETURNING key`, append(args, hash, s.config.Retention.Microseconds())...).Scan(&inserted)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		if ctx.Err() != nil {
			return nil, false, errors.Join(ErrInProgress, err)
		}
		return nil, false, err
	}
	if inserted == "" {
		var storedHash, result []byte
		var expired, done bool
		err = ex.QueryRow(ctx, `SELECT fingerprint,result,expires_at<=now(),completed FROM pangolin_idempotency.results WHERE tenant_id=$1 AND scope_kind=$2 AND scope_id=$3 AND principal_kind=$4 AND principal_id=$5 AND operation=$6 AND key=$7`, args...).Scan(&storedHash, &result, &expired, &done)
		if err != nil {
			return nil, false, errors.Join(ErrInProgress, err)
		}
		if expired {
			return nil, false, ErrExpired
		}
		if !equal(hash, storedHash) {
			return nil, false, ErrConflict
		}
		if !done {
			return nil, false, ErrInProgress
		}
		result, err = s.config.Unprotect(result)
		return result, true, err
	}
	result, err := work(ctx)
	if err != nil {
		return nil, false, err
	}
	if len(result) > s.config.MaxBodyBytes {
		return nil, false, uow.Fail(ctx, uow.ErrOptionsInvalid)
	}
	protected, err := s.config.Protect(result)
	if err != nil {
		return nil, false, err
	}
	_, err = ex.Exec(ctx, `UPDATE pangolin_idempotency.results SET result=$8,completed=true WHERE tenant_id=$1 AND scope_kind=$2 AND scope_id=$3 AND principal_kind=$4 AND principal_id=$5 AND operation=$6 AND key=$7`, append(args, protected)...)
	return result, false, err
}
func equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type Result struct {
	State   string
	Payload []byte
}

// Query must run in a new authorized read UOW after Unknown. Missing is unknown,
// never proof of rollback. It cannot read another principal or owner's result.
func (s *Store) Query(ctx context.Context, operation, key string) (Result, error) {
	b, err := s.identity(ctx, operation, key)
	if err != nil {
		return Result{}, err
	}
	ex, err := postgres.Current(ctx, false)
	if err != nil {
		return Result{}, err
	}
	var raw []byte
	var expired, done bool
	err = ex.QueryRow(ctx, `SELECT result,expires_at<=now(),completed FROM pangolin_idempotency.results WHERE tenant_id=$1 AND scope_kind=$2 AND scope_id=$3 AND principal_kind=$4 AND principal_id=$5 AND operation=$6 AND key=$7`, b.Ref.TenantID, b.Ref.Kind, b.Ref.ID, b.Principal.Kind, b.Principal.ID, operation, key).Scan(&raw, &expired, &done)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{State: "unknown"}, nil
	}
	if err != nil {
		return Result{}, err
	}
	if expired {
		return Result{State: "expired"}, nil
	}
	if !done {
		return Result{State: "in_progress"}, nil
	}
	raw, err = s.config.Unprotect(raw)
	return Result{State: "succeeded", Payload: raw}, err
}

//go:embed migrations/*.sql
var Migrations embed.FS
