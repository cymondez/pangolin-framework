# Root 裝配與建構子 DI 開發指南

2026-11-25 的設計審查修正（Root module loading + constructor DI）在 `codex/design-review-corrections` 分支以同一版本推進；權威語意在[實作規範書 §2](../../design/panGOlin-framework_實作規範書.md)、[§4.3](../../design/panGOlin-framework_實作規範書.md)，工作與驗收映射在[里程碑規劃書](../../design/panGOlin-framework_里程碑規劃書.md)。本指南描述目前程式碼的組裝方式、重現入口與尚未完成的邊界；一切狀態以同版 Gate 為準，本檔不宣告 Verified。

## 涉及的工作包與擁有者

- 檔案協調紀錄在 `.workflow/root-module-di/`：[plan.md](../../.workflow/root-module-di/plan.md)、[packets/core-api.md](../../.workflow/root-module-di/packets/core-api.md)（組合契約，實作於 `kernel/module/services.go`）、[packets/loader-api.md](../../.workflow/root-module-di/packets/loader-api.md)（Type 閉環 loader）、[packets/review-decisions.md](../../.workflow/root-module-di/packets/review-decisions.md)（契約擁有者中心可視性修正）與 [packets/integration-feedback.md](../../.workflow/root-module-di/packets/integration-feedback.md)（INTEGRATION 所需修正）。
- `kernel/module`：Root Type 參照 與 loader（`root_types.go`、`root_loader.go`）在任何 hook 前完成閉環驗證；`services.go` 承載建構子組合引擎——`RootBuilder`（SetDefault／Services／Provide／Replace）、`Services`（Add／Replace／Export／Bind／ReplaceBinding／ReplaceContract 與 `Singleton`／`Transient`／`Operation`／`As` 選項）、`Composition`（Seal／LifecycleResolver）；`root_scope.go` 的 `module.OpenScope` 把受限生命週期 Resolver 轉成模組自有的有界明示操作 `di.Scope`（owner 固定、可視集合不變、Close 由呼叫端持有）。
- `kernel/runtime`（`root.go`）：`RootHost.StartRoot` 與 [`runtime.StartRoot`](../../kernel/runtime/root.go) 入口；既有 `Host.Start(ctx, []module.Module)` 明示清單裝配維持原語意不變。Host 逐 hook 交出 trusted `RootBuilder`，hook 返回（含錯誤與 panic）後 builder 與 engine 註冊把手即撤銷。
- `kernel/app`（`app.go`）：`Run(ctx, root, opts...)` 以有界預算與必備 StartupRules 預設啟動，Sources／StartupChecks／Participants 為 opt-in。
- `tests/rootmodules`：跨套件 Root 模式回歸（無外部服務，隨 `go test ./...` 執行）；`examples/moduleapp` 是可執行的正式 Root 示範。

## Root 模式語意重點

1. 以模組 Type 參照宣告根模組；loader 遞推 `DependsOn() []module.Ref` 閉合、依 Type 去重，並在任何 hook／建構子前拒絕重名、循環、無效 Type 與版本不符（錯誤攜帶相依路徑）。
2. 三階段完整屏障：全部模組 PreConfigure → 全部 Configure → 全部 PostConfigure；Host 逐 hook 開啟 provenance 視窗並在 hook 返回（含錯誤與 panic）後關閉，hook 之外與視窗過期後的操作一律拒絕（engine 逐 owner epoch 把關，host 端 Revoke 等待進行中操作落地）。
3. 一般模組在 `Configure` 內 `b.Services().Add(NewX)`；預設 Transient、`Singleton`／`Operation` 明示 opt-in。解析鍵是精確具象型別（建構子參數型別），介面契約經擁有者 `Export`、跨擁有者貢獻經 `As`（實作模組須直接相依契約擁有者）、`Bind` 僅用於真實 ambiguity（契約擁有者，或直接相依擁有者與所選實作兩者的其他組合成員）、`ReplaceContract`／`Replace` 保留契約 Identity 並記錄原因。特殊 DB／Broker 整合走 `Provide`／`Replace` 命名 token，來源／直接相依／公開契約明示。
4. Seal 在任何 Factory 執行前判決 missing／ambiguity／laundering／可視性；Start／Stop 都只拿逐模組受限 Resolver，沒有全域 Resolver 逃逸（Stop 內解析未授權契約會使 Stop 失敗並完整反序清理一次）。Operation 服務在 Root 模式只能經 `module.OpenScope(ctx, resolver)` 取得：owner 於建立時固定、可視集合與該 Resolver 完全相同、Close 由呼叫端負責且冪等；nil／typed-nil／非生命週期 Resolver（含 raw Container 或委派包裝）一律拒絕。StartupChecks 與 RequiredScopeKinds 維持既有全域組合責任。
5. `kernel/app.Run` 供簡單正式應用；生成應用直接使用 `runtime.New` ＋ `runtime.StartRoot` 以保留既有 drain 參數與 Serve 錯誤通道。CLI 生成的框架資源由獨立可 import 的 `foundation.Module` 擁有：`module add --depends persistence/authorization/organization` 別名翻譯為 foundation Type 邊，框架服務（UOW、`foundation.ScopeAccess`、Publisher、Idempotency）以 typed constructor 注入；`foundation.Profile` 在同一契約擁有者下替換 Profile 實作；`internal/app` 根模組只保留 HTTP／drain 組裝。把生成領域樣本 `bookstore` 宣告為依賴會被明確拒絕（不靜默略過）；舊「app root 自行擁有框架資源」形狀在引擎可視性檢查失敗，遷移方式是 user module 改宣告 foundation Type 邊，不是 import app root（避免 package cycle，理由見 tests/rootmodules 重現測試）。

## 執行與驗證入口

```powershell
go test ./kernel/module ./kernel/runtime ./kernel/app ./tests/rootmodules
go -C examples/moduleapp run .     # 規劃輸出與 Root 生命週期診斷；MODULEAPP_DEADLINE=2s 可在非互動環境完整觀察反序 Stop
# Root 模式與 foundation 組合驗證（無 DB 亦可重現 seal 與 Start 解析）
python artifacts/root-module-di-2026-11-25/integration/followup_seal_check.py
python artifacts/root-module-di-2026-11-25/integration/moduleapp_run_check.py
```

生成應用（`pangolin new`）兩種 Profile 的入口已改走 Root 裝配：`modules/registry.go` 產出 `UserRoot` 聚合，`foundation` 套件以 `foundation.Module` 平台模組擁有全部 DB／Broker／事件資源與公開框架契約（`foundation.UnitOfWork`／`foundation.Scopes`／`foundation.Registry` 等 typed 契約，pool 以 `foundation.DB`／`foundation.AuthorityDB` 具象 handle 區分同一 Go 型別的兩個 pool），`foundation.profile` 模組只以 `ReplaceContract`／`b.Replace` 在相同契約擁有者下替換 Profile 實作；`internal/app` 根模組保留 HTTP／drain 組裝並在 `Start` 內完成 deployment 相容檢查，不再擁有任何平台資源、也不再要求 user module 依賴 app root（避免 app↔modules 匯入循環）。invalid graph 先驗證才 seal，任何資源 factory 都只在 seal 執行、cleanup 由 Container 單一擁有。`module add` 的 framework 依賴名稱（`persistence`／`authorization`／`organization`）翻譯為 `module.Of[foundation.Module]()` Type 邊，不靜默省略；`bookstore` 是生成網域示例名稱，使用者模組請依賴 foundation 能力或已註冊使用者模組。Runtime 模板以 `python scripts/template_bundle.py` 與 source 同步，`--check` 驗證 drift。

OpenAPI／資料契約不變：`scripts/template_openapi.py` 與 `scripts/m5_templates.py` 的情境檢查未調整；`module add` 產生的模組為 Root 模式（`ModuleMetadata`／`DependsOn`／`RootBuilder`），framework 依賴名稱翻譯為 foundation Type 邊，pangolin.json 中的 framework 名稱保留給 migration 排序。

## 驗收與狀態

- `A01/root-*`（14 項）、`A02/root-*`（14 項）、`A46/root-*`（72 項）、`A74/root-*`（1 項）家族沿 `A63/permit-*` 前例以加法具名情境追蹤；101 項情境已於 2026-11-25 註冊進 `tests/contract/subtests.json`（三種 Profile 皆 required，契約總數 532＝421 原情境＋10 `A63/permit-*`＋101 root-*），並映射到既有測試函式——loader／引擎／私有介面／OpenScope 情境映射 `kernel/module` 的 `TestRootLoader*`／`TestServices*`／`TestPrivate*`／`TestScope*`，生命週期／受限 Resolver（Start 與 Stop 都算）／停機情境映射 `kernel/runtime` 的 `TestRoot*` 與 `TestLegacyHostStartUnchanged`，app 進入情境映射 `kernel/app` 的 `TestRun*`，跨套件情境映射 `tests/rootmodules`（含 pre-fix 重現三例），生成 typed-framework injection 映射 `internal/scaffold:TestGeneratedTypedFrameworkInjection`，`A74/root-required-scope-kinds-rejected` 映射 `kernel/runtime:TestRootRequiredScopeKindsWithoutRegistryFails`（M2 映射）；映射清單見 `tests/contract/m1.json`（M1 映射 147 項）與 `tests/contract/m2.json`（M2 closure 134）。
- 案例**尚未**通過同版 Gate／累積回歸，不計入通過集合，正式狀態仍為 `In progress`／cumulative QA pending，本輪不更新任何 Verified；先前報告的全部映射缺口（Stop 期限定 Resolver 端到端負面／正向、非擁有者正向 Bind＋缺邊負面、私有介面注入、RequiredScopeKinds 歸屬）已由實際落地測試補齊並按 primary 裁定註冊（`A74/root-*` 家族），未使用假映射。
- INTEGRATION 已交付並通過本輪 focused 驗證：`examples/moduleapp`（規劃、三階段屏障、菱形去重、契約 Export／解析、反序 Stop 全部在執行輸出可觀察）、CLI 兩 Profile 生成物 build／test／vet、`module add` Root 模式模板與 UserRoot 聚合、foundation 組合的無 DB runtime seal／Start 解析驗證（見本檔驗證入口）、`tests/rootmodules` 情境（既有＋pre-fix 重現三例）、kernel/app 與 runtime Root 測試（含 Stop 可視性、並行 SetDefault、StartRoot host 檢查、builder 自身 Replace）、`module.OpenScope` 六個 suite、私有介面 25 項情境、模板 bundle 同步與 docs。正式 clean commit 發行 Gate 仍是 Verified 前提；QA 環境已由 primary 以 setup-manifest.json 檢查 healthy。
- 舊 431 個具名情境歷史不變；契約總數 532 由 contract matrix 精確推導（421 原情境＋10 `A63/permit-*`＋101 root-*），既有 Gate 斷言已同步更新，歷史 M1–M6 證據數值與 commit 維持原狀。
- **2026-11-25 freeze-blocking 私有介面修正已落地：** 既有引擎曾要求同模組內部介面也要 `Export` 才能注入，強迫內部服務公開並破壞 §4.3 邊界。權威語意在實作規範 §2.2「擁有者本模組私有介面注入」＋§2.3 bullet＋§4.3 clarifier；引擎補正與測試已完成（`services_private.go`；四項缺口修復、`kernel/module` raw coverage 91.87%，見 `results/CORE-PRIVATE-FINAL.md`）：`Add(NewRepo)`／`Add(NewUseCase)` 等同模組內部介面**無 Export 自動推論**、私有鍵為消費 module＋reflect.Type（同一介面在兩模組私下獨立）、已載入公開契約的擁有者相依要求優先於 owner-local matching、私有 missing／ambiguity／dangling binding 在 factory 前報出（診斷不洩漏外部模組型別）、真實 ambiguity 以私有 `Bind` 選自家候選且無須 export、生命週期 Resolver／`module.OpenScope` 使用同一 owner-private 映射且不暴露其他模組私有介面、低階自訂名稱維持精確身分且 never 成為私有候選、`Bind` 後 `Export` 促升回公開規則、低階 factory 宣告的 canonical 私有相依經 guarded alias 轉譯而未宣告的 resolver 存取被拒。語意說明見 [kernel/module README（Owner-private 介面注入）](../../kernel/module/README.md)；[packets/private-interfaces.md](../../.workflow/root-module-di/packets/private-interfaces.md) 保留為當時實作契約存檔；一般模組建構子註冊仍是單行 `Add`，沒有任何強制 export 的權宜路徑。
