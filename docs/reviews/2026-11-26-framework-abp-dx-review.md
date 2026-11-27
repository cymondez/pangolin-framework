# panGOlin Framework Code Review

**審查日期：** 2026-11-26  
**審查基準：** `develop`，`3db4958`  
**審查目標：** 找出核心正確性、公開介面、程式寫法與開發者體驗可改善處，並將修改方向對準 C# ABP Framework 的「日常開發應簡單、底層能力仍可擴充」使用感。  
**狀態：** Design Review / 尚未納入正式需求或 Verified evidence。

## 摘要

框架的核心能力已具備明確的模組 DAG、建構子 DI、UOW、Tenant／Scope 隔離、Outbox／Inbox、Profile 與測試基礎。安全與交易限制大多已有規範與測試保護，不宜為了方便而放寬。

目前與 ABP 式使用感的主要落差，不在功能數量，而在日常 API 經常暴露底層機制：

- DI resolution 回傳 `any`，需手動 token 與 type assertion。
- Application UOW helper 不支援直接回傳值，正式範例因此繞過 helper。
- lifecycle scope 以寬泛 `di.Resolver` 作為參數，實際只接受未公開的實作型別。
- CLI 可快速生成大型專案，但缺少框架 Runtime 的安全升級流程。
- 常見註冊、進階 contract replacement 與 escape hatch 混在同一個介面。
- 生成模組只有骨架，尚未形成可直接延伸的 vertical slice。

建議先完成四個基礎 façade：typed DI resolution、typed DI registration、可回傳值的 Command／Query helper、由 Runtime 提供 operation scope。這四項可以加法式導入，不需要破壞現有底層 Port。

## 評分概覽

| 面向 | 現況 | 目標 |
| --- | ---: | --- |
| 安全與交易機制 | 9/10 | 維持嚴格契約，改善 API 可讀性 |
| Module 與 DI 能力 | 7/10 | 常用路徑完全型別安全、低 ceremony |
| Application Service 順手度 | 6/10 | Command／Query 直接回傳結果，範例全面採用 |
| CLI 首次生成 | 8/10 | 保持快速生成，增加可工作的模組 preset |
| 升級體驗 | 3/10 | 有可預覽、可衝突檢查、可回滾的 upgrade command |
| 錯誤可診斷性 | 7/10 | 問題、原因、修復方式集中在錯誤訊息中 |
| 文件與實際推薦寫法 | 7/10 | 全部教學、template、範例使用同一高階 API |

## Findings

### P1-01：UOW 錯誤比較可能把不同錯誤誤認成相同 sentinel

證據：

[`persistence/uow/errors.go`](../../persistence/uow/errors.go#L24-L38) 的 `failure` 只保存錯誤碼字串，`Is(target error)` 使用 `target.Error() == e.code`。因此任何建立為 `errors.New("UOW_READ_ONLY")` 的外部錯誤，都可能與框架 `ErrReadOnly` 匹配成功。

影響：

- `errors.Is` 的語意不是錯誤身分，而是字串內容。
- Adapter 或應用自行建立相同訊息時，可能被錯誤映射成 UOW 錯誤。
- 日後調整錯誤文字可能連帶改變分類行為。

修改方向：

1. `failure` 保存原始 sentinel error，或同時保存 sentinel 與穩定 `Code()`。
2. `Is()` 優先比對原始 sentinel 身分；若需要跨 process 字串代碼，只在明確的 decode boundary 重建錯誤。
3. 保留 `Error` 介面的 `Code()`、`ScopeID()`、`OperationID()`、`Outcome()`，避免破壞 transport 使用方式。
4. `errorCode()` 改用明確 sentinel 比對，不以任意實作相同文字的錯誤作為分類依據。

建議驗證：

新增測試，建立 `foreign := errors.New(uow.ErrReadOnly.Error())`，驗證 `errors.Is(failure, foreign)` 為 false、`errors.Is(failure, uow.ErrReadOnly)` 為 true；再執行 `go test ./persistence/uow`。

### P1-02：DI 常用解析路徑退化成 `any` 與手動 type assertion

證據：

[`kernel/di/contracts.go`](../../kernel/di/contracts.go#L38) 的 `Resolver.Resolve` 回傳 `(any, error)`。[`examples/moduleapp/app/module.go`](../../examples/moduleapp/app/module.go#L29-L40) 必須先取得 `any`，再手動 `value.(contracts.Catalog)`。

影響：

- IDE 無法在呼叫點顯示期望介面。
- 錯誤在 runtime 才出現。
- 每個模組重複 token、resolve、assertion、type error 訊息。
- 相較 ABP 的注入式與 generic 服務取得，認知與程式碼成本偏高。

修改方向：

保留低階 reflection engine，但新增高階 façade：

```go
catalog, err := module.Resolve[contracts.Catalog](ctx, rt.Services())
```

也提供適合初始化與 callback 的型別化包裝，例如：

```go
func Resolve[T any](ctx context.Context, resolver di.Resolver) (T, error)
func MustResolve[T any](ctx context.Context, resolver di.Resolver) T
```

`MustResolve` 應只供組態或測試 fixture 使用，production hook 應使用回傳 error 的版本。

最小修改切片：

1. 在 `kernel/module` 或新的 public `kernel/service` façade 中增加 generic wrapper。
2. wrapper 內部仍呼叫既有 `Resolve(module.Contract[T]())` 與型別檢查。
3. 先只改一個 hello／moduleapp 範例與一組 contract test。
4. 驗證後再替換 scaffold 與教學中的手動 assertion。

相容策略：

加法式 API，不刪除或改變 `di.Resolver`。

### P1-03：Command／Query helper 不能直接回傳值，正式範例因此繞過它

證據：

[`application/operation.go`](../../application/operation.go#L10-L36) 只接受回傳 `error` 的 callback。[`examples/bookstore/application/books.go`](../../examples/bookstore/application/books.go#L45-L65) 直接呼叫 `manager.Within`；查詢還需透過 closure 修改外部變數。

影響：

- `application.Command/Query` 名義上是共用政策，實際代表性 Application Service 卻沒有使用它。
- tracing 與共用 policy 可能因採用直連 UOW 而遺失。
- 回傳值的 closure 需要先宣告命名變數，增加狀態與錯誤路徑。
- Command 與 Query 的推薦入口不夠接近 ABP Application Service 的自然寫法。

修改方向：

Go 目前不允許 generic method，因此以 package function 提供 result helper：

```go
book, err := application.CommandResult(ctx, manager, func(work context.Context) (Book, error) {
    if err := repository.Insert(work, book); err != nil {
        return Book{}, err
    }
    return book, nil
})

book, err := application.QueryResult(ctx, manager, func(work context.Context) (Book, error) {
    return repository.Get(work, id)
})
```

並讓原本的 `Command`／`Query` 內部委派給 shared helper，避免兩套交易與 tracing 行為漂移。

最小修改切片：

1. 新增 generic result helper 與單元測試。
2. 將 Bookstore `Create`、`Get` 改用 helper。
3. 更新 scaffold application template。
4. 搜尋 `manager.Within`，只保留框架內部確實需要低階 options 的位置。

驗證：

- `go test ./application ./examples/bookstore/application ./transport/http`
- 驗證 callback error、commit error、marshal-after-commit 與 tracing outcome 不變。

### P1-04：Operation scope 的型別契約過寬，實際接受型別卻極窄

證據：

[`kernel/module/root_scope.go`](../../kernel/module/root_scope.go#L37-L48) 接受任意 `di.Resolver`，進入函式後卻以 concrete assertion 限制為 `*lifecycleResolver`。合法的 resolver wrapper 也會被拒絕。

影響：

- 編譯器無法阻止錯誤參數。
- 使用者必須知道未公開 concrete type 的存在與來源。
- 錯誤訊息只能說「unsupported resolver」，不能由介面本身表達能力。

修改方向：

優先將 scope 能力移到 module lifecycle view：

```go
type Runtime interface {
    Services() di.Resolver
    OpenScope(context.Context) (di.Scope, error)
    Config() config.Reader
}
```

或者定義 `type LifecycleServices interface { di.Resolver; OpenScope(context.Context) (di.Scope, error) }`，讓 `Runtime.Services()` 回傳該 capability。

安全限制：

- 保持 owner binding 固定。
- 不暴露 raw container 或 engine。
- `Close` 仍具冪等、有界 cleanup 語意。
- wrapper 不得因 façade 而繞過 module visibility。

相容策略：

保留 `module.OpenScope(ctx, resolver)` 一段過渡期，內部可委派新介面；新範例與 scaffold 只使用新的 Runtime capability。

### P1-05：生成專案沒有可 review、可回滾的 Framework 升級流程

證據：

[`internal/scaffold/cli.go`](../../internal/scaffold/cli.go#L224-L307) 將 framework runtime bundle 複製進生成專案。實測新專案包含 151 個檔案。[`docs/site/content/reference/upgrading.md`](../../docs/site/content/reference/upgrading.md#L45) 要求整套更新 `framework/`、manifest，或重新生成專案。

影響：

- 升級 diff 很大，難以判斷框架來源與應用修改的界線。
- 使用者容易只更新部分 bundle，造成 runtime、template、CLI 版本不一致。
- 沒有 upgrade dry-run、衝突分類或 rollback plan。

修改方向：

短期增加 CLI workflow：

```text
pangolin upgrade plan
pangolin upgrade --dry-run
pangolin upgrade runtime
pangolin upgrade verify
```

`plan` 至少輸出：

- 目前與目標 Framework commit／version。
- runtime 檔案的 unchanged／modified／conflict。
- API inventory、migration、設定鍵差異。
- 使用者修改的 bundle 檔案。
- 預估的 git 工作樹與建置驗證步驟。

正式版應逐步改為獨立 Go module dependency，而不把整套 runtime 複製進應用。生成專案只保留專案程式、migration、profile 設定與明確版本鎖。

安全限制：

升級不得自動覆寫使用者編輯；遇到衝突必須產生 patch／清單並失敗，不可靜默保留或替換。

### P2-01：Services 介面混合日常註冊與進階 replacement 機制

證據：

[`kernel/module/services.go`](../../kernel/module/services.go#L42-L62) 同時包含 `Add`、`Replace`、`Export`、`Bind`、`ReplaceBinding`、`ReplaceContract`。這些方法各自依賴不同的 ownership、dependency 與 contribution 規則。

影響：

- 初學者無法從介面區分常用與例外 API。
- `Bind`、`ReplaceBinding`、`ReplaceContract` 名稱接近但語意不同。
- lifetime 經 option 組合，錯誤只能在 runtime 驗證。

修改方向：

建立 progressive disclosure：

```go
type Services interface {
    Add(constructor any, opts ...AddOption) error
    Replace(constructor any, reason string, opts ...AddOption) error
    Export(contract di.Token) error
    Contracts() ContractServices
}

type ContractServices interface {
    Bind(contract di.Token, impl any) error
    ReplaceBinding(contract di.Token, impl any, reason string) error
    ReplaceContract(contract di.Token, constructor any, reason string, opts ...AddOption) error
}
```

另外增加 convenience API：

```go
AddSingleton(constructor any, opts ...AddOption) error
AddTransient(constructor any, opts ...AddOption) error
AddOperation(constructor any, opts ...AddOption) error
```

lifetime API 應與 `As(...)` 分離，讓互斥選項不易被同時傳入。

相容策略：

舊 `Services` 在預發行階段可直接調整；若需平滑測試與 template，先以加法 helper 實作，再於下一個 break window 收斂介面。

### P2-02：`module add` 只產生骨架，未建立可工作的 vertical slice

證據：

[`internal/scaffold/cli.go`](../../internal/scaffold/cli.go#L470-L521) 產生 `ModuleMetadata`、空的 `Configure` 與大量註解，真正 constructor 仍需開發者自行完成。

影響：

- 使用者必須先理解 DI owner、framework capability 與 module dependency 才能得到第一個可測試服務。
- CLI 生成成功不等於 module 已達成最低行為。
- 高階框架的「生成即可延伸」感不足。

修改方向：

新增 preset，而不是讓預設 skeleton 更複雜：

```text
pangolin module add catalog --depends persistence,authorization
pangolin module add catalog --preset service
pangolin module add catalog --preset crud
```

`service` 生成：

- module registration
- application contract 與 implementation
- constructor injection
- 失敗路徑
- 單元測試
- README

`crud` 再加上 Domain、Repository Port／Adapter、migration skeleton、HTTP route 與 OpenAPI。

驗證：

每個 preset 在 clean temp directory 必須通過 `go test ./...`，生成後不應出現未使用 import 或 TODO 才能編譯。

### P2-03：`doctor` 目前是 static inventory check，不是完整健康診斷

證據：

[`internal/scaffold/cli.go`](../../internal/scaffold/cli.go#L597-L644) 檢查 Go、Compose、bundle 與 checksum，但沒有檢查專案 build、設定或 live service。

影響：

- 輸出成功可能被理解成「應用已可啟動」。
- 實際缺 DB、JWT、migration 或 schema 相容性時，問題只在較晚階段出現。

修改方向：

將結果分層：

```text
[PASS] project files
[PASS] runtime manifest
[PASS] go build ./...
[SKIP] database, requires --live
[SKIP] migrations, requires --live
```

增加 `doctor --live`，只在明確要求時檢查 Compose 狀態、DB 連線、migration ledger 與 API readiness。預設 `doctor` 不應啟動服務。

## 建議的修改優先序

### Wave 1：先消除正確性與例外傳染

1. 修正 UOW error sentinel 比對。
2. 為 error matching 增加 regression test。
3. 將 `CommandResult`／`QueryResult` 加進 application package。
4. Bookstore 先改為使用 result helper，確認行為不變。

Exit criteria：

- 所有 UOW classification 對字串碰撞均可預測。
- Bookstore application 不再重複手寫主要 UOW boundary。

### Wave 2：建立 ABP 式的高階使用面

1. 增加 `module.Resolve[T]`。
2. 增加 `Runtime.OpenScope` 或 typed lifecycle services。
3. 增加 `AddSingleton`、`AddTransient`、`AddOperation` facade。
4. moduleapp、Bookstore、docs 與 scaffold 改用 façade。

Exit criteria：

- 常見 Application Service 不需直接處理 `any`、type assertion 或 concrete lifecycle resolver。
- 低階 engine 仍可供 runtime 與框架測試使用。

### Wave 3：改善生成與診斷

1. `module add --preset service`。
2. `doctor --live` 分層診斷。
3. CLI invalid dependency 列出有效候選。
4. generated README 明示第一個驗證命令與其意義。

Exit criteria：

- 生成的 module 在新增任何使用者程式前即可 build／test。
- doctor 每個成功項目只宣稱實際檢查的層級。

### Wave 4：建立可維運的升級路徑

1. `upgrade plan`／`--dry-run`／manifest diff。
2. 使用者修改與 framework-owned files 的衝突規則。
3. 升級前後 API、migration、settings compatibility check。
4. 正式版移除 app-local full runtime bundle，改為鎖定 Go module source。

Exit criteria：

- 使用者可在不修改工作樹的情況下先看到升級影響。
- Framework 檔案不會被 CLI 靜默覆寫。
- upgrade 後可直接執行固定驗證與 rollback 步驟。

## API 設計原則

後續修改應維持以下原則：

1. **低階正確性與高階 ergonomics 分離。** `di`、UOW internals 維持能力完整；日常 module/application 使用 high-level façade。
2. **安全限制不能為了減少程式碼而變寬。** module visibility、owner scope、transactional UOW、RLS 與 Outbox 契約不退回 permissive default。
3. **相同能力只有一個推薦入口。** docs、examples、tests、scaffold 應使用同一套 façade，底層 API 只在框架實作與特殊案例出現。
4. **Simple case production-ready。** 最簡單的 `Resolve[T]`、`CommandResult`、`module add --preset service` 不能只是 demo-only shortcut。
5. **錯誤必須包含問題、分類與修復路徑。** 例如不知道 dependency 時列出有效名稱，而不是只說 validation failure。
6. **CLI 必須保留使用者檔案。** 所有生成、升級與 overwrite 都採 exclusive／manifest／conflict policy。
7. **公開 API 改動要有 migration note。** 即使目前是預發行，仍應記錄 old → new 使用方式，降低範例與 template 漂移。

## 不列為缺陷的項目

下列行為已由規範或測試支持，不應在 ergonomics 工作中改寫：

- `RequiresNew` 保留父操作已 admission 的 Scope snapshot，不重建授權。見 [`security/scope/admission_test.go`](../../security/scope/admission_test.go#L58-L99)。這是短 UOW snapshot 語意；真正的改善點是讓此限制在 API 與文件中更顯眼。
- Job deadline 已過期仍可被驗證為合法歷史 Job；worker/store 會在 claim 階段以 DB 時間判斷 dead／cancelled。見 [`platform/jobs/jobs.go`](../../platform/jobs/jobs.go#L75-L89) 與 [`platform/jobs/postgres/store.go`](../../platform/jobs/postgres/store.go#L91-L105)。
- Outbox publish 透過 `uow.Fail` 將無法持久化或 scope 不符的 event failure 標為 rollback-only。這是可靠事件與業務交易一致性的必要限制。

## 驗證證據

本次 review 未修改 production 程式碼。

### CLI 實測

隔離 temp directory 中執行：

```powershell
go build -o <temp>\pangolin.exe ./cmd/pangolin
pangolin new reviewapp --template modular-monolith
pangolin doctor
go test ./...
```

實際結果：

- 新專案建立時間：`655 ms`
- 生成檔案數：`151`
- `doctor`：通過
- 生成專案 `go test ./...`：通過

### Repository 實測

```text
go test ./kernel/module ./kernel/di ./kernel/app ./application ./persistence/uow ./internal/scaffold
```

實際結果：全部通過，`internal/scaffold` 約 `13.574s`。

## 建議下一個實作切片

先做一個可驗證、低風險的 vertical slice：

1. 在 `application` 增加 `CommandResult`／`QueryResult`。
2. 在 `kernel/module` 增加 `Resolve[T]`。
3. 將 Bookstore `Create`／`Get` 改用這兩個新 façade。
4. 增加同字串不同 sentinel 的 UOW regression test。
5. 同步更新 Bookstore、moduleapp、Docusaurus quickstart。
6. 執行 focused tests、lint／vet，以及 API inventory 檢查。

這個切片不改資料 schema、交易語意、module visibility 或 transport contract，但能直接驗證「較少程式碼、相同安全邊界」是否成立。
