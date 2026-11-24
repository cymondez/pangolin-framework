package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cymondez/pangolin-framework/kernel/config"
	"github.com/cymondez/pangolin-framework/platform/cache"
	"github.com/cymondez/pangolin-framework/platform/features"
	"github.com/cymondez/pangolin-framework/platform/settings"
	"github.com/cymondez/pangolin-framework/security/scope"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// This console demo has an explicit identity. HTTP applications instead use
// their authenticated Tenant/Scope pipeline and current authority.
func demoContext(tenant string) (context.Context, *scope.Registry) {
	identity := func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, tenant, true
	}
	r, err := scope.NewRegistry(identity, scope.TenantScopeProvider{
		Identity: identity,
		Permission: func(_ context.Context, _ scope.ScopeBinding, action string) error {
			switch action {
			case "books.read", "settings.write":
				return nil
			default:
				return scope.ErrDenied
			}
		},
	})
	must(err)
	ctx, err := r.Bind(context.Background(), scope.ScopeRef{TenantID: tenant, Kind: "tenant", ID: tenant})
	must(err)
	return ctx, r
}

func main() {
	ctx, permissions := demoContext("tenant-a")
	other, _ := demoContext("tenant-b")
	memory, err := cache.NewMemory("bookstore", "0.1.0", 32)
	must(err)
	resource := cache.Resource{Module: "catalog", Kind: "book", ID: "book-1"}
	must(memory.Set(ctx, resource, []byte("tenant-a book"), time.Minute))
	_, hit, err := memory.Get(other, resource)
	must(err)
	fmt.Println("Tenant cache isolated:", !hit)

	notificationFails := false
	provider := settings.NewMemoryProvider(settings.PublisherFunc(func(context.Context, settings.Change) error {
		if notificationFails {
			return errors.New("demo notification unavailable")
		}
		return nil
	}))
	binding, _ := scope.Current(ctx)
	_, err = provider.Put(ctx, settings.Layer{}, "theme", json.RawMessage(`"sand"`))
	must(err)
	_, err = provider.Put(ctx, settings.Layer{Ref: binding.Ref}, "theme", json.RawMessage(`"blue"`))
	must(err)
	_, err = provider.Put(ctx, settings.Layer{Ref: binding.Ref}, "book_limit", json.RawMessage(`10`))
	must(err)
	_, err = provider.Put(ctx, settings.Layer{Ref: binding.Ref, User: binding.Principal}, "book_limit", json.RawMessage(`50`))
	must(err)
	jsonConfig, err := config.ParseJSON([]byte(`{"Settings":{"Channels":[{"Name":"mail"},{"Name":"sms"}]}}`))
	must(err)
	source, err := settings.WithConfiguration(provider, jsonConfig, nil)
	must(err)
	settingValues, err := settings.New(source, memory, nil, time.Minute,
		settings.Definition{Key: "Channels:0:Name", Default: json.RawMessage(`"none"`), Inherit: true},
		settings.Definition{Key: "theme", Default: json.RawMessage(`"light"`), Inherit: true},
		settings.Definition{Key: "book_limit", Default: json.RawMessage(`100`), Inherit: true},
		settings.Definition{Key: "editing", Default: json.RawMessage(`true`)},
	)
	must(err)
	theme, err := settingValues.Resolve(ctx, "theme")
	must(err)
	fmt.Println("Tenant setting:", string(theme.Value))
	channel, err := settingValues.Resolve(ctx, "Channels:0:Name")
	must(err)
	fmt.Println("JSON array setting:", string(channel.Value))
	layers, err := settingValues.Layers(ctx, "theme")
	must(err)
	fmt.Println("Setting layers:", layers)

	flags, err := features.New(settingValues, permissions, map[string]features.Definition{
		"books.limit": {Setting: "book_limit", Permission: "books.read", Kind: features.Quota, Ceiling: true, ScopeKinds: []string{"tenant"}},
		"books.edit":  {Setting: "editing", Permission: "books.write", Kind: features.Boolean},
	})
	must(err)
	limit, err := flags.Evaluate(ctx, "books.limit")
	must(err)
	fmt.Println("Effective quota:", limit.Quota)
	_, err = flags.Evaluate(ctx, "books.edit")
	fmt.Println("Feature cannot grant permission:", errors.Is(err, scope.ErrDenied))

	notificationFails = true
	version, updateErr := settingValues.Update(ctx, permissions, "theme", json.RawMessage(`"cream"`))
	fmt.Println("Stored update despite notification failure:", version > 0 && updateErr != nil)
	theme, err = settingValues.Resolve(ctx, "theme")
	must(err)
	fmt.Println("Version converged:", theme.Version == version && string(theme.Value) == `"cream"`)
}
