// Package app 是 moduleapp 示範的根模組（Root Module）。
// 它只宣告直接相依的業務模組（catalog、pricing），共用的底層 store 會由 Loader 自動去重規劃；
// 並且透過受限的模組解析器，只能看見自己直接相依模組所 Export 的公開契約。
package app

import (
	"context"
	"fmt"

	"example.com/moduleapp/catalog"
	"example.com/moduleapp/catalog/contracts"
	"example.com/moduleapp/pricing"
	"github.com/pangolin-framework/pangolin/kernel/module"
)

// Module 是整個應用的最頂層根模組；本身不註冊業務實作，只負責聚合相依與啟動入口。
type Module struct{}

func (m *Module) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "app", Version: "0.1.0"}
}

// DependsOn 宣告兩個直接相依模組：catalog 與 pricing。
// 兩者共同依賴的 store.Module 會由 Loader 沿圖閉合，且只規劃並啟動一次。
func (m *Module) DependsOn() []module.Ref {
	return []module.Ref{module.Of[catalog.Module](), module.Of[pricing.Module]()}
}

func (m *Module) Start(ctx context.Context, rt module.Runtime) error {
	// 步驟 1：示範透過 rt.OpenScope(ctx) 開啟綁定當前模組身分的 Operation Scope，
	// 並用 defer scope.Close(ctx) 確保操作結束後確實釋放資源（重複呼叫 Close 無害）。
	scope, err := rt.OpenScope(ctx)
	if err != nil {
		return err
	}
	defer scope.Close(ctx)

	// 步驟 2：app 直接相依 catalog，因此能成功解析 catalog Export 的 contracts.Catalog；
	// 相對地，pricing 未 Export 的內部 *pricing.Prices 對 app 來說不可見。
	application, err := module.Resolve[contracts.Catalog](ctx, scope)
	if err != nil {
		return err
	}
	fmt.Printf("start app: catalog ready with %d entr%s via %T\n",
		application.Len(), plural(application.Len()), application)
	return nil
}

func (m *Module) Stop(context.Context, module.Runtime) error {
	fmt.Println("stop app")
	return nil
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
