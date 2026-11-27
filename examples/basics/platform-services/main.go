package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pangolin-framework/pangolin/kernel/config"
	"github.com/pangolin-framework/pangolin/platform/cache"
	"github.com/pangolin-framework/pangolin/platform/features"
	"github.com/pangolin-framework/pangolin/platform/settings"
	"github.com/pangolin-framework/pangolin/security/scope"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// 建立本機命令列教學用的租戶 Scope context（模擬已通過認證與授權的請求）。
// 正式 HTTP 應用必須改由認證 Pipeline、Tenant/Scope Resolver 與當前權威來源建立。
func demoContext(tenant string) (context.Context, *scope.Registry) {
	identity := func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, tenant, true
	}
	r, err := scope.NewRegistry(identity, scope.TenantScopeProvider{
		Identity: identity,
		// 這裡刻意只允許 books.read 與 settings.write，不允許 books.write，
		// 用來在步驟 4 驗證「Feature 開啟也絕不能授予沒有的 Permission」。
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
	// 步驟 1：建立兩個不同租戶（tenant-a、tenant-b）的受控 context，
	// 驗證 Scoped Memory Cache 會自動以完整 Scope 隔離快取鍵，彼此絕不互看。
	ctx, permissions := demoContext("tenant-a")
	other, _ := demoContext("tenant-b")
	memory, err := cache.NewMemory("bookstore", "0.1.0", 32)
	must(err)
	resource := cache.Resource{Module: "catalog", Kind: "book", ID: "book-1"}
	must(memory.Set(ctx, resource, []byte("tenant-a book"), time.Minute))
	_, hit, err := memory.Get(other, resource)
	must(err)
	fmt.Println("Tenant cache isolated:", !hit)

	// 步驟 2：準備分層 Settings Provider（含可模擬失效通知失敗的 Publisher），
	// 分別寫入 Global 層（"sand"）與 Tenant 層（"blue"）的 theme，
	// 以及 Tenant 上限（10）與 User 期望值（50）的 book_limit。
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

	// 步驟 3：掛入 JSON 啟動設定來源（Config 層，與可動態修改的 Global 層分開），
	// 示範 JSON 陣列路徑 Settings:Channels:0:Name 與五層繼承解析順序。
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

	// 步驟 4：建立 Features 評估器，驗證兩大規則：
	// - Ceiling: true 讓 Tenant 上限 10 壓住 User 的 50，最終有效配額為 10。
	// - editing 雖為 true，但使用者缺少 books.write 權限，Evaluate 仍必須拒絕！
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

	// 步驟 5：模擬更新設定成功寫入來源後「快取失效通知失敗」的情境：
	// Update 會回傳非零版本與錯誤，而後續 Resolve 因核對來源原子快照版本，仍能正確收斂至最新值 "cream"。
	notificationFails = true
	version, updateErr := settingValues.Update(ctx, permissions, "theme", json.RawMessage(`"cream"`))
	fmt.Println("Stored update despite notification failure:", version > 0 && updateErr != nil)
	theme, err = settingValues.Resolve(ctx, "theme")
	must(err)
	fmt.Println("Version converged:", theme.Version == version && string(theme.Value) == `"cream"`)
}
