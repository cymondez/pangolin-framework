---
title: 功能模組（Module）
description: 理解 panGOlin 如何用編譯期 Module struct 與 Type 相依圖（DAG）劃分業務邊界，並自動規劃啟停順序。
---

> **你在架構中的位置**：**`[ 模組邊界與相依圖（Module DAG）]`** `→ 組態與 DI 註冊 → 依序 Start → Ready → 反序 Stop`
>
> **前置建議**：先完成[第一個應用](../getting-started/quickstart.mdx)並看過[架構總覽](../concepts/architecture.md)。

## 為什麼需要功能模組（Module）？

在沒有模組概念的 Go 專案中，隨著業務增加，大家通常會把所有初始化邏輯通通塞進 `cmd/api/main.go`：

- `main.go` 動輒上千行，手動決定「先連 DB、再建 Repo、再建 Service A、再建 Service B、再建 HTTP」，順序一錯就 nil pointer panic。
- 停機時常常忘記反序關閉（例如 HTTP 還在處理請求，底層 DB 連線池已經先被 `defer pool.Close()` 關掉了）。
- 不同團隊成員可以任意跨套件 `import` 彼此內部結構，久而久之整個單體變成牽一髮動全身的大泥球。

panGOlin 的 **Module** 用極低的語法成本解決這三件事：
1. **明確所有權**：每個業務領域（如 `books`、`orders`、`billing`）各自有一個 Module struct，負責宣告自己的設定預設、DI 服務與啟停工作。
2. **編譯期相依圖（DAG）**：模組用 Go 泛型型別參照 `module.Of[T]()` 宣告自己依賴誰，啟動前自動拓撲排序並拒絕重名、循環、缺依賴與版本不符。
3. **跨模組可見性隔離**：每個模組只能看見「自己註冊的服務」以及「直接相依模組明示 Export 的契約」，沒有隱式全域存取。

---

## 核心心智模型：從根模組自動閉合相依圖

使用 Root 裝配（`app.Run(ctx, root)`）時，你**不需要**手動維護一份超長的模組陣列。只要把最頂層的根模組交給 `app.Run`，Loader 會沿著 `DependsOn()` 自動展開整張有向無環圖（DAG）：

```text
                [ app (根模組) ]
                 /            \
                ▼              ▼
          [ catalog ]      [ pricing ]
                 \            /
                  ▼          ▼
                 [   store    ]    ◀── 被兩個模組共用，但只規劃、啟動、停止一次！

啟動順序（由底向上）：Layer 0 [store] ──▶ Layer 1 [catalog, pricing] ──▶ Layer 2 [app]
停機順序（由頂向下）：app ──▶ pricing ──▶ catalog ──▶ store
```

同層模組之間會依模組名稱穩定排序，確保在 Windows、Linux、CI 與正式環境的啟動順序完全可預測。

---

## 1. 定義一個標準模組

模組就是一個普通的 Go struct，只需要提供模組身分（名稱與版本），其餘所有生命週期方法都是**可選的**（用不到就不用寫）：

```go title="internal/books/module.go"
package books

import (
	"context"
	"fmt"

	"github.com/cymondez/pangolin-framework/kernel/module"
)

type Module struct{}

// 步驟 1：提供模組唯一名稱與語意版本（用於去重、拓撲排序與版本相容性檢查）。
func (m *Module) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "books", Version: "0.1.0"}
}

// 步驟 2（可選）：在 Configure 階段宣告預設設定與註冊服務建構子。
func (m *Module) Configure(b module.RootBuilder) error {
	return b.SetDefault("books.page_size", "20")
}

// 步驟 3（可選）：在所有模組完成組態與 DI 封存後執行啟動邏輯。
func (m *Module) Start(ctx context.Context, rt module.Runtime) error {
	fmt.Println("Books module is ready")
	return nil
}

// 步驟 4（可選）：在優雅停機階段停止模組自行擁有的工作（反序執行）。
func (m *Module) Stop(ctx context.Context, rt module.Runtime) error {
	fmt.Println("Books module stopped")
	return nil
}
```

> **簡寫技巧**：如果模組 struct 不需要自訂方法來動態計算 Metadata，可以直接嵌入 `module.Base`（如[第一個應用](../getting-started/quickstart.mdx)所示：`&helloModule{Base: module.Base{Name: "hello", Version: "0.1.0"}}`），就不用手寫 `ModuleMetadata()`。

---

## 2. 宣告模組相依與版本約束

當 `books` 必須等底層 `storage` 模組完成組態與啟動後才能運作時，實作 `DependsOn() []module.Ref`：

```go title="宣告 Type 相依與版本約束"
func (m *Module) DependsOn() []module.Ref {
	return []module.Ref{
		// module.Of[T]() 直接以 Go 型別參照目標模組：
		// - 拼錯名稱在編譯期就會報錯，不必等到執行期
		// - Requires("=0.1.0") 可選地宣告版本相容約束
		module.Of[storage.Module]().Requires("=0.1.0"),
	}
}
```

啟動前 Loader 會嚴格檢查四種錯誤，任一成立立即拒絕啟動（不執行任何模組的 `Configure` 或 `Start`）：
- **重名模組**：兩個不同模組宣告相同的 `Name`。
- **缺漏相依**：宣告依賴的模組不存在或沒有被納入圖中。
- **循環相依**：例如 A 依賴 B、B 又依賴 A（強制你把共用契約抽出成獨立模組 C）。
- **版本不相容**：目標模組的 `Version` 不滿足 `Requires(...)` 約束。

完整的多模組啟停範例見 **[應用啟動與停止](application-startup.mdx)**；包含跨模組介面契約的完整示範則可直接執行框架內的 `examples/moduleapp`（詳見 **[依賴注入](dependency-injection.mdx)**）。

---

## 3. 五個生命週期階段，什麼程式碼該放哪裡？

請特別注意 panGOlin 的**全階段推進規則**：Runtime 會先把**所有**模組的 `PreConfigure` 全部跑完，才跑所有模組的 `Configure`，接著才跑所有 `PostConfigure`，封存 DI 後才依序 `Start`——**絕對不是**逐一模組從頭跑到尾。

| 生命週期階段 | 執行時機與順序 | 你應該做的事 ✅ | 不要做的事 ❌ |
|---|---|---|---|
| `PreConfigure(b)` | 全部模組先跑，早於 `Configure` | 宣告需要給其他模組在 `Configure` 讀取或覆寫的基礎預設與前置註冊 | 不要解析還沒封存的服務 |
| `Configure(b)` | 全部模組主要註冊階段 | **90% 的註冊寫在這裡**：`b.SetDefault(...)`、`module.AddSingleton`／`AddTransient`／`AddOperation`、`b.Services().Export(...)` | 不要開啟網路連線、不要啟動 goroutine、不要保存 `b.Services()` 留到之後用 |
| `PostConfigure(b)` | 全部 `Configure` 結束後 | 需要覆寫／綁定前面模組註冊結果的收尾調整（例如 `Replace`、`Bind`） | 不要把日常業務註冊塞在這裡 |
| `Start(ctx, rt)` | DI 封存且 `StartupChecks` 通過後，**依相依順序**（底層先） | 透過 `module.Resolve[T]` 取得服務、啟動 HTTP Listener、啟動背景 Worker | 不要在 `Start` 裡回頭修改設定或註冊新服務 |
| `Stop(ctx, rt)` | 收到停止訊號且 Drain 後，**反序執行**（上層先） | 停止模組自己在 `Start` 啟動的 goroutine 或 listener | 若服務建構子已回傳 `Cleanup` 函式，容器會自動收尾，**不要重複關閉同一資源兩次** |

---

## 4. 常見誤區提醒

- **誤區 1：每個小資料夾都拆成一個 Module**
  Module 對應的是**業務領域或獨立基礎設施邊界**（例如 `books`、`orders`、`organization`、`postgres`），不是每個 Go package 都要寫一個 Module。單一業務模組內部的 `domain`、`application`、`infrastructure`、`transport` 共用同一個 Module 即可。
- **誤區 2：期待熱插拔（Dynamic Plugin）**
  panGOlin 刻意只做**編譯期明確註冊**、不提供執行期動態載入模組。這樣所有的相依圖、型別契約與權限能力都能在啟動第 0 秒完全靜態驗證。
- **誤區 3：忘記宣告 `RequiredScopeKinds`**
  如果你的業務模組需要組織層級隔離（例如 `org.organization`），應透過模組宣告所需 Scope Kind，讓 Runtime 在啟動前就核對對應的 Scope Provider 已安裝（詳見[資源 Scope 與組織擴充](../security/resource-scopes.md)）。

---

## 下一步

學會把系統切成模組後，接著閱讀 **[依賴注入（DI）](dependency-injection.mdx)**，了解模組內建構子如何自動注入參數，以及模組之間如何透過 `contracts` 與 `Export` 安全共享服務。
