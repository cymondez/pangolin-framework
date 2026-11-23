// Package audit separates transactional required audit from independent security logs.
package audit

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sync"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/security/scope"
)

var ErrInvalid = errors.New("AUDIT_INVALID")
var identifier = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,160}$`)

// Record has no token, raw request, SQL, error text or arbitrary payload fields.
type Record struct {
	Action   string        `json:"action"`
	Resource string        `json:"resource"`
	Result   string        `json:"result"`
	Trace    string        `json:"trace"`
	Duration time.Duration `json:"duration_ns"`
}

func valid(r Record) bool {
	return identifier.MatchString(r.Action) && identifier.MatchString(r.Resource) && identifier.MatchString(r.Result) && identifier.MatchString(r.Trace) && r.Duration >= 0
}

// Recorder uses only the active write executor. Required audit failure fails UOW.
type Recorder struct{}

func (Recorder) Record(ctx context.Context, r Record) error {
	if !valid(r) {
		return ErrInvalid
	}
	b, ok := scope.Current(ctx)
	if !ok {
		return scope.ErrDenied
	}
	ex, err := postgres.Current(ctx, true)
	if err != nil {
		return err
	}
	_, err = ex.Exec(ctx, `INSERT INTO pangolin_audit.records(id,tenant_id,scope_kind,scope_id,principal_kind,principal_id,action,resource,result,trace,duration_ns) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, rand.Text(), b.Ref.TenantID, b.Ref.Kind, b.Ref.ID, b.Principal.Kind, b.Principal.ID, r.Action, r.Resource, r.Result, r.Trace, int64(r.Duration))
	return err
}

type Entry struct {
	At        time.Time          `json:"at"`
	Principal scope.PrincipalRef `json:"principal"`
	Scope     scope.ScopeRef     `json:"scope"`
	Record    Record             `json:"record"`
}

// SecurityLog is independent of DB transactions. The owner closes its writer.
type SecurityLog struct {
	writer   io.Writer
	mu       sync.Mutex
	identity scope.IdentityReader
}

func NewSecurityLog(writer io.Writer, identity ...scope.IdentityReader) (*SecurityLog, error) {
	if writer == nil || len(identity) > 1 || len(identity) == 1 && identity[0] == nil {
		return nil, ErrInvalid
	}
	log := &SecurityLog{writer: writer}
	if len(identity) == 1 {
		log.identity = identity[0]
	}
	return log, nil
}
func (l *SecurityLog) Write(ctx context.Context, r Record) error {
	if ctx == nil || l == nil || !valid(r) {
		return ErrInvalid
	}
	b, bound := scope.Current(ctx)
	if !bound && l.identity != nil {
		p, _, verified := l.identity(ctx)
		if verified && p.ID != "" && (p.Kind == "user" || p.Kind == "service") {
			b.Principal = p
		}
	}
	entry := Entry{At: time.Now().UTC(), Principal: b.Principal, Scope: b.Ref, Record: r}
	l.mu.Lock()
	defer l.mu.Unlock()
	return json.NewEncoder(l.writer).Encode(entry)
}

//go:embed migrations/*.sql
var Migrations embed.FS
