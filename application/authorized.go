package application

import (
	"context"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/policy"
)

// AuthorizedCommand is an explicit sensitive-use-case boundary, including direct
// application calls. ResourcePolicy executes inside the admitted transaction.
func AuthorizedCommand(ctx context.Context, m uow.UnitOfWorkManager, a policy.Authorizer, action string, resource policy.ResourcePolicy, work func(context.Context) error) error {
	if work == nil || m == nil {
		return uow.ErrOptionsInvalid
	}
	if err := policy.Require(ctx, a, action, nil); err != nil {
		return err
	}
	return Command(ctx, m, func(c context.Context) error {
		if err := policy.Require(c, a, action, resource); err != nil {
			return err
		}
		return work(c)
	})
}
