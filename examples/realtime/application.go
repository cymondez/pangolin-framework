package main

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cymondez/pangolin-framework/application"
	"github.com/cymondez/pangolin-framework/modules/realtime"
	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/security/scope"
)

type demoApplication struct {
	db       *demoResources
	messages realtime.Publisher
}

func newDemoApplication(db *demoResources, messages realtime.Publisher) *demoApplication {
	return &demoApplication{db, messages}
}

type demoDestination struct {
	Tenant, Kind, ID, Channel, UserID string
	Rollback                          bool
}

func parseDestination(args []json.RawMessage) (demoDestination, error) {
	var request demoDestination
	if len(args) == 0 {
		return request, realtime.ErrInvalid
	}
	if err := json.Unmarshal(args[0], &request); err != nil {
		return request, errors.Join(realtime.ErrInvalid, err)
	}
	return request, nil
}

func (d demoDestination) ref() scope.ScopeRef {
	return scope.ScopeRef{TenantID: d.Tenant, Kind: d.Kind, ID: d.ID}
}

func selectDemoScope(_ string, args []json.RawMessage) (scope.ScopeRef, error) {
	request, err := parseDestination(args)
	return request.ref(), err
}

func (a *demoApplication) echo(ctx context.Context, args []json.RawMessage) (any, error) {
	var value string
	if len(args) != 1 || json.Unmarshal(args[0], &value) != nil {
		return nil, realtime.ErrInvalid
	}
	return value, application.Query(ctx, a.db.units, func(context.Context) error { return nil })
}

func (a *demoApplication) notify(ctx context.Context, args []json.RawMessage) (any, error) {
	if len(args) != 2 {
		return nil, realtime.ErrInvalid
	}
	request, err := parseDestination(args)
	if err != nil {
		return nil, err
	}
	var id string
	err = application.Command(ctx, a.db.units, func(work context.Context) error {
		id, err = a.messages.Publish(work, realtime.Destination{Scope: request.ref(), Channel: request.Channel, UserID: request.UserID}, args[1])
		if err != nil {
			return err
		}
		ex, err := postgres.Current(work, true)
		if err != nil {
			return err
		}
		_, err = ex.Exec(work, "INSERT INTO realtime_demo.notices(id, tenant_id, scope_kind, scope_id) VALUES ($1,$2,$3,$4)",
			id, request.Tenant, request.Kind, request.ID)
		if err == nil && request.Rollback {
			return errors.New("demo rollback requested")
		}
		return err
	})
	return id, err
}
