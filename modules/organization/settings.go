package organization

import (
	"context"

	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
)

// Parent implements the neutral Settings ancestry Port using current authority.
// Inherited product configuration never grants access to Enterprise content.
func (s *Store) Parent(ctx context.Context, b scope.ScopeBinding) (scope.ScopeRef, bool, error) {
	current, ok := scope.Current(ctx)
	if !ok || current != b || b.Ref.Kind != "org.organization" {
		return scope.ScopeRef{}, false, scope.ErrDenied
	}
	fresh := b
	if uow.Check(ctx, false) != nil {
		var err error
		fresh, err = s.Resolve(ctx, b.Principal, b.Ref)
		if err != nil {
			return scope.ScopeRef{}, false, err
		}
	}
	snapshot, err := s.verify(fresh)
	if err != nil {
		return scope.ScopeRef{}, false, err
	}
	if snapshot.ParentID == "" {
		return scope.ScopeRef{}, false, nil
	}
	return scope.ScopeRef{TenantID: b.Ref.TenantID, Kind: "org.enterprise", ID: snapshot.ParentID}, true, nil
}
