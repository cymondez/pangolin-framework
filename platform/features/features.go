// Package features evaluates typed product availability after security permission.
package features

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/cymondez/pangolin-framework/platform/settings"
	"github.com/cymondez/pangolin-framework/security/policy"
	"github.com/cymondez/pangolin-framework/security/scope"
)

var ErrInvalid = errors.New("FEATURE_INVALID")

type Kind string

const (
	Boolean Kind = "boolean"
	Quota   Kind = "quota"
	Enum    Kind = "enum"
)

type Definition struct {
	Setting, Permission string
	Kind                Kind
	// Ceiling intersects booleans, takes the minimum quota, or caps an enum by
	// its explicit ascending Options order. Lower layers cannot loosen a parent.
	Ceiling    bool
	Options    []string
	ScopeKinds []string // Empty permits any trusted kind; nonempty is an allowlist.
}
type Resolver interface {
	Resolve(context.Context, string) (settings.Result, error)
}
type Service struct {
	settings    Resolver
	authorizer  policy.Authorizer
	definitions map[string]Definition
}

func New(r Resolver, a policy.Authorizer, definitions map[string]Definition) (*Service, error) {
	if r == nil || a == nil {
		return nil, ErrInvalid
	}
	s := &Service{r, a, map[string]Definition{}}
	for key, d := range definitions {
		if key == "" || d.Setting == "" || d.Permission == "" || d.Kind != Boolean && d.Kind != Quota && d.Kind != Enum || d.Kind == Enum && len(d.Options) == 0 {
			return nil, ErrInvalid
		}
		seen := map[string]bool{}
		for _, v := range d.Options {
			if v == "" || seen[v] {
				return nil, ErrInvalid
			}
			seen[v] = true
		}
		d.Options = slices.Clone(d.Options)
		seen = map[string]bool{}
		for _, kind := range d.ScopeKinds {
			if strings.TrimSpace(kind) != kind || kind == "" || seen[kind] {
				return nil, ErrInvalid
			}
			seen[kind] = true
		}
		d.ScopeKinds = slices.Clone(d.ScopeKinds)
		s.definitions[key] = d
	}
	return s, nil
}

type Value struct {
	Boolean bool
	Quota   int64
	Enum    string
	Version uint64
}

func (s *Service) Evaluate(ctx context.Context, key string) (Value, error) {
	d, ok := s.definitions[key]
	if !ok {
		return Value{}, ErrInvalid
	}
	if err := policy.Require(ctx, s.authorizer, d.Permission, nil); err != nil {
		return Value{}, err
	}
	b, _ := scope.Current(ctx)
	if len(d.ScopeKinds) > 0 && !slices.Contains(d.ScopeKinds, b.Ref.Kind) {
		return Value{}, scope.ErrDenied
	}
	r, err := s.settings.Resolve(ctx, d.Setting)
	if err != nil {
		return Value{}, err
	}
	values := []json.RawMessage{r.Value}
	if d.Ceiling {
		values = r.Candidates
	}
	if len(values) == 0 {
		return Value{}, ErrInvalid
	}
	result := Value{Version: r.Version}
	for i, raw := range values {
		switch d.Kind {
		case Boolean:
			var v bool
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &v) != nil {
				return Value{}, ErrInvalid
			}
			if i == 0 {
				result.Boolean = v
			} else {
				result.Boolean = result.Boolean && v
			}
		case Quota:
			var v int64
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &v) != nil || v < 0 {
				return Value{}, ErrInvalid
			}
			if i == 0 || v < result.Quota {
				result.Quota = v
			}
		case Enum:
			var v string
			if json.Unmarshal(raw, &v) != nil || !slices.Contains(d.Options, v) {
				return Value{}, ErrInvalid
			}
			if i == 0 || slices.Index(d.Options, v) < slices.Index(d.Options, result.Enum) {
				result.Enum = v
			}
		}
	}
	return result, nil
}
