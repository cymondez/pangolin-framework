package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/cymondez/pangolin-framework/security/policy"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/cymondez/pangolin-framework/tenancy"
)

// 以下兩個教學用 fixture 只用來在本機無外部服務的環境示範 Permission 判斷邏輯，
// 不會驗證真實憑證。正式 HTTP 應用須改接驗證後的 authn/identity 與真實當前資格來源（Authority）。
func demoIdentity(context.Context) (scope.PrincipalRef, string, bool) {
	return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant-a", true
}

type demoAuthority struct{}

// Check 模擬查詢當前有效的租戶成員資格，並回傳權威 revision（撤權或變更時 revision 會變動）。
func (demoAuthority) Check(_ context.Context, principal scope.PrincipalRef, tenant string) (string, error) {
	if principal != (scope.PrincipalRef{ID: "alice", Kind: "user"}) || tenant != "tenant-a" {
		return "", scope.ErrDenied
	}
	return "demo-membership-1", nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// 步驟 1：建立 Tenant Resolver，比對登入身分、候選租戶與當前資格來源（Authority）。
	authority := demoAuthority{}
	resolver, err := tenancy.NewResolver(demoIdentity, authority)
	if err != nil {
		return err
	}
	ctx, err := resolver.Resolve(context.Background(), "tenant-a", "tenant-a")
	if err != nil {
		return err
	}

	// 步驟 2：定義此示範的權限規則（正式環境須由可信權威來源讀取，不可取自請求參數）：
	// - books.read：角色層允許 → 允許
	// - books.write：即使 Role / Tenant / System 全部 Allow，但 User 明確 Deny → 必須拒絕！
	rules := map[string]policy.Rules{
		"books.read": {Roles: []policy.Decision{policy.Allow}},
		"books.write": {
			User:   policy.Deny,
			Roles:  []policy.Decision{policy.Allow},
			Tenant: policy.Allow,
			System: policy.Allow,
		},
	}

	// 步驟 3：建立 Scope Registry 並註冊 Tenant Provider，將權限判斷接上 policy.Allowed。
	registry, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{
		Authority: authority,
		Permission: func(_ context.Context, _ scope.ScopeBinding, permission string) error {
			if !policy.Allowed(rules[permission]) {
				return scope.ErrDenied
			}
			return nil
		},
	})
	if err != nil {
		return err
	}

	// 步驟 4：將可信的 ScopeRef (TenantID, Kind, ID) 綁定到 context。
	ctx, err = registry.Bind(ctx, scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	if err != nil {
		return err
	}

	// 步驟 5：在敏感 Application Use Case 內呼叫 policy.Require 檢查動作權限
	// （即使 HTTP 入口已驗過，敏感 Use Case 本身仍應檢查，避免其他入口直接呼叫時漏驗）。
	if err = policy.Require(ctx, registry, "books.read", nil); err != nil {
		return err
	}
	fmt.Println("read allowed")

	// 驗證 User 明確 Deny 優先於一切 Allow：books.write 必須回傳 scope.ErrDenied。
	if err = policy.Require(ctx, registry, "books.write", nil); !errors.Is(err, scope.ErrDenied) {
		return fmt.Errorf("expected explicit user deny, got %v", err)
	}
	fmt.Println("write denied")

	// 步驟 6：驗證核心安全原則——即使某個 Feature Flag 已開啟，也絕不會賦予使用者原本沒有的權限。
	featureEnabled := true
	if featureEnabled {
		if err = policy.Require(ctx, registry, "books.write", nil); !errors.Is(err, scope.ErrDenied) {
			return fmt.Errorf("feature must not grant write permission, got %v", err)
		}
		fmt.Println("feature enabled: write denied")
	}
	return nil
}
