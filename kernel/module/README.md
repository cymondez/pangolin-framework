# Module：註冊與相依規劃

`Module` 是編譯期明確註冊的功能單位。此套件只驗證及規劃模組圖；實際生命週期由 [runtime](../runtime/README.md) 執行。公開契約見 [contracts.go](contracts.go)，實作見 [registry.go](registry.go)。

```go
registry := module.NewRegistry()
err := registry.Register(module.Module{
    Name: "books", Version: "0.1.0",
    DependsOn: []module.Dependency{{Name: "database", VersionConstraint: ">=0.1.0 <0.2.0"}},
})
// 必須也註冊 database；檢查每次 Register 的 err，再呼叫 Plan。
plan, err := registry.Plan()
```

Name 不可空白或重複。Version 採 SemVer；依賴只接受 `=0.1.0` 或 `>=0.1.0 <0.2.0`，預發行只接受精確相等；build metadata 不影響版本比較。不接受 wildcard／caret／tilde／OR。缺漏、版本不符及循環會回傳包含相依路徑的錯誤。

Plan 採逐層 Kahn 排序，每層依名稱排序。Register 複製切片，Plan 回傳 detached snapshot；呼叫端修改切片不會改寫 Registry。Runtime 必須在任何 configure／Start 前取得合法 Plan。

各模組的 Start 失敗時，該模組負責收回尚未交付的部分資源；已成功啟動的模組由 Runtime 反序 Stop。若資源由 DI Factory 的 Cleanup 所擁有，Stop 不得再關閉同一資源。完整階段順序與停機方式見 Runtime 說明。

驗證：`go test ./kernel/module ./kernel/runtime`。A01 涵蓋非法圖、版本約束及穩定排序；A02 驗證生命週期與失敗清理。M1 不提供熱插拔。

## Root 裝載（Type 相依閉環）

Root 裝載以模組 Type 參照為圖邊，實作見 [root_types.go](root_types.go) 與 [root_loader.go](root_loader.go)，API 約定見 `.workflow/root-module-di/packets/loader-api.md`。

```go
// 模組宣告 Type 相依（可選）與基本中繼資料（可選；同 package 可放多個模組）
func (m *AuthModule) DependsOn() []module.Ref { return []module.Ref{module.Of[StoreModule]()} }
func (m *AuthModule) ModuleMetadata() module.Metadata {
    return module.Metadata{Name: "auth", Version: "0.2.0"}
}

plan, err := module.LoadRoot(&AuthModule{}) // 根模組以 instance pointer 提供，closure 依 Type 去重
```

`LoadRoot` 在執行任何 hook 或建構任何資源之前完成全部驗證：typed nil／非 pointer 根、invalid Ref（pointer／interface／未命名 struct）、`DependsOn()`／`ModuleMetadata()` panic、Type 層循環（含自參照）、穩定名稱重複、SemVer 無效版本，以及**每條**邊的顯式約束（含 Type 去重後的重複共用邊）。約束語法沿用 Registry：`=0.1.0` 或 `>=0.1.0 <0.2.0`；未約束的邊固定為解析版本的精確 pin `=<version>`，版本本身一律驗 SemVer。

回傳 `RootPlan{Plan, Instances}`：`Plan` 是既有 `Registry.Plan()` 的 detached snapshot（逐層穩定名稱排序、hook 欄位為 nil），`Instances` 將穩定名稱映射到實際 instance——Root 以原 pointer 保留，相依項為 `reflect.New` 的零值 instance，菱形相依僅建一次。預設名稱是**完整 package import path 加型別名**（如 `example.com/app/auth.AuthModule`，不用 `reflect.Type.String()` 的短名，泛型具現化保留 `[args]` 識別），預設版本 `0.1.0`；`Metadata`／`Base` 零值欄位回退預設，嵌入 `module.Base` 即滿足 `MetadataProvider`（回傳防禦複本）。型別 nil `Ref` 與自訂 `Ref` 方法 panic 會轉為載入錯誤，失敗載入回傳空 `RootPlan`，不建任何 instance。hook 介面與 Root runtime 接線由 Runtime 擁有，此 loader 不執行、也不需要它們。

驗證：`go test ./kernel/module`（含 `TestRootLoader*` 家族）。

## Generic façade 與註冊 façade

日常解析優先使用 `module.Resolve[T](ctx, resolver)`：它使用既有 `module.Contract[T]()` 建立 canonical token，呼查既有 Resolver，並在型別不符時回傳包含期待／實際型別的錯誤。`module.MustResolve[T]` 是 panic-on-error 的 convenience，只適合組態或測試 fixture；production lifecycle hook 應處理 `Resolve` 的 error。

註冊仍走既有 `Services.Add`：`module.AddSingleton(services, constructor, opts...)`、`module.AddTransient(...)` 與 `module.AddOperation(...)` 是 lifetime façade，可與 `module.As(contract)` 等 `AddOption` 組合；多個 lifetime option 相互衝突仍由既有 registration validation 在 `Add` 時拒絕。若模組同時註冊實作並公開介面契約，可用 `module.AddAndExportSingleton[Contract]`、`AddAndExportTransient[Contract]`、`AddAndExportOperation[Contract]` 一步完成 `Add(..., As(Contract[Contract]()))` 與 `Export(Contract[Contract]())`。多項註冊時可用錯誤累積式流暢註冊器 `module.Register(b.Services()).Singleton(...).Transient(...).Operation(...).Export(...).Bind(...).Replace(...).ReplaceContract(...).ReplaceBinding(...).Err()`，首個錯誤後短路記錄。可逐步採用，不會改變 `Services` 或低階 provider 介面。

## Root 建構子組合引擎（services.go）

`services.go` 實作 Root 模式的 constructor DI 引擎，輸入是 `LoadRoot` 的 `RootPlan`；ROOT runtime 的整合接線見 `.workflow/root-module-di/packets/core-api.md`。

```go
comp, err := module.NewComposition(plan, cleanupTimeout)   // 引擎擁有自己的 di.Registry
svc, err := comp.Services("auth")                          // owner-stamped registrar（provenance）
_ = svc.Add(NewJWTVerifier, module.Singleton())            // 建構子註冊；鍵=確切回傳型別
_ = svc.Export(module.Contract[scope.RegistryPort]())      // 契約 owner 唯一 export
_ = svc.Bind(module.Contract[scope.RegistryPort](), implJWT{}, ) // 僅化解真實 ambiguity（owner 或直接相依兩方的模組可 bind）
container, err := comp.Seal(ctx)                           // 全部驗證在執行任何建構子之前；之後 canonical di.Container
resolver, err := comp.LifecycleResolver("auth")            // Start/Stop 的逐模組受限 resolver
```

- **建構子**：`func(...) T`、`(T, error)`、`(T, func(context.Context) error, error)`；不可回傳介面、不可 variadic、最多一個 `context.Context` 參數（特殊非服務注入）。預設 Transient；`module.Singleton()`／`module.Operation()` 明示 opt-in；`module.As(token)` 是跨 owner 對契約的顯式貢獻（Seal 檢查實作模組直接相依契約 owner）。
- **契約識別**：`module.Contract[T]()`／`module.Service[T]()` 由型別身分導出確定性非空名稱（`pkgpath.Name`，指標加 `*` 前綴，泛型具現化保留參數）；As／Bind／ReplaceContract／Export 的契約語意只接受 canonical token；具名低階 Provider 保留其確切 `(Name, Type)`，`Export` 該具名 token 即 deliberate bridge，不會被悄悄改名。
- **Seal 驗證順序**（不執行任何 factory）：binding 目標存在 → 契約解析（低階/建構子衝突、缺實作、ambiguity 列出 `module=… type=…`、binding 僅限化解真實 ambiguity、型別須符合候選）→ owner-private 介面推論（missing／ambiguity／dangling binding 全部在此決定，詳見下節）→ Singleton 經任何邊（含 Transient 與契約／private alias 委派）觸及 Operation 即拒絕 → 每個宣告相依的可視性（owner 或 directly-depend+export，錯誤具名消費者/型別/應相依 owner）→ 產生 di providers（契約與 private alias 以 delegation provider 轉址到選中實作，建構子參數由 guarded 包裝器換成 alias token）→ **釋放引擎鎖後**才 `di.Registry.Seal`（eager Singleton 在鎖外執行，stash 的舊 registrar 立即失敗，不會 deadlock）。Seal 一次性；驗證失敗視同已 Seal。
- **Provenance**：`Services(owner)` 每次回傳新 handle，`Revoke(owner)` 使先前 handle 立即失效並等待進行中操作結束（session 鎖 + epoch 在同一把鎖下檢查與變更，撤銷不會與寫入交錯）；`Composition.Provide/Replace` 是低階具名 token 逃生口，同樣以 owner 蓋章、需要非空 reason 記錄於 `ReplacementLog()`。
- **取代語意**：`Replace` 只換自己模組先前註冊的同型別建構子；跨模組 `Replace`（建構子或低階 Provider）僅允許取代**已 export** 的公開識別，且呼叫端須直接相依 export owner（契約 owner）；token 與 export 身分不變、實作來源移轉（`ownerOfToken` 回 export owner，factory 相依檢查用來源 owner），未 export 的私有服務一律不得被外人取代。`ReplaceBinding` 換自家 binding；`ReplaceContract` 是中立方實作的跨 owner 取代，且**必須存在明確既有實作**（先前 As 貢獻、owner 本地的結構候選或精確契約識別的低階 Provider）——首次貢獻一律 `Add+As`，無既有目標即拒絕；呼叫端須為 owner 或直接相依 owner（不須相依實作模組），保留原契約 owner/export 身分、來源與 reason 記錄於 `ReplacementLog()`。排除狀態是**契約特定**（`excluded[key]`）：取代契約 I 不會移除同一具象對契約 J 的預設服務，具象本身仍可解析；多個舊候選的取代是一次性的獨占取代且與註冊順序無關。`As` 與 `ReplaceContract` 在**註冊時**驗證 `concrete.Implements(interface)`，不相容關聯立即拒絕、絕不進入候選或執行任何建構子。
- **生命週期 resolver**：Start/Stop 取得 `LifecycleResolver(name)`——僅可解析自身與直接相依且已 export 的 token；Operation 服務必須經顯式 scope 解析；resolver 不暴露 container、不拆解返回值、不自動 dispose。
- 驗證：`go test ./kernel/module -run TestServices`（建構子形狀、可視性診斷、跨 owner As／Bind／ReplaceContract、laundering、provenance windows、清理次序、canonical token、plan 驗證）。`-race` 在本機 Windows 需要 cgo gcc，正式 race 證據依 Gate 環境。

## Owner-private 介面注入（services_private.go）

同一模組內 `Add(NewRepo)` + `Add(NewUseCase)`（UseCase 以 module 私有 Repository 介面為參數）**不需要 Export** 即可組合，實作見 [services_private.go](services_private.go)。補正是引擎行為，不改任何公開簽名：

- **推論規則**：模組建構子宣告介面型別參數，且該 key 未被任何 loaded public contract（`Export`）認領、也沒有以精確識別註冊的 entry 時，引擎在 Seal 從**該模組自己的建構子產物**中結構推論：恰一個相容具象即成為 alias；零個以「missing provider <介面>」語意失敗（具名消費者 + 介面型別，不列出外部模組型別）、多個為 ambiguity 失敗。推論只考慮**建構子產物**——As 專屬建構子、低階具名 Provider 與外部模組型別永不入選。
- **鍵控**：private alias 以（消費模組, 介面 reflect.Type 鍵）識別，container 身分用引擎管理的名稱空間（canonical name + `\x00private\x00` + owner）。同一 Go 介面被兩個模組各自私下使用時得到兩個獨立 alias、各自實作、互不可見；任何模組都不能宣告、bind 或 Resolve 引擎管理的 alias 識別（registration 與可視性兩處 fence 阻擋含 `\x00` 標記的 token）。sealed container 維持 host 擁有（StartupChecks 既有責任），模組可及的解析入口一律是受限 lifecycle resolver／`OpenScope` scope。
- **公開優先**：key 一旦被任一模組 Export 成 public contract，該型別的解析全部走公開規則（owner 或直接相依 owner 後透過契約供應者），本地相容具象**不遮蔽**公開 owner 的規則；未直接相依時維持原有「應宣告直接相依」診斷。Bind 與 Export 的註冊順序無關：先 Bind 後 Export 的 private binding 在 Seal 被促升（promotion）回公開規則，衝突 binding 拒絕。
- **私有 Bind**：真實 ambiguity 時呼叫 `svc.Bind(module.Contract[介面](), ownCandidate)` 選擇自家候選，契約仍不 export；單一候選時 private Bind 為 redundant 錯誤、型別不符候選時為 does-not-match 錯誤、無消費建構子時為 no-module-exported 錯誤（與公開規則的拒絕語意平行）。`ReplaceBinding` 對 private binding 同樣可用（需要 reason，記錄於 ReplacementLog）。
- **Seal 前失敗**：missing／ambiguity／dangling binding 全部在 provisionLocked 決定，先於任何 factory；laundering（Singleton→Operation）與迴圈檢查把 private alias 邊視為與執行期 factory 圖完全相同的邊（alias 節點帶選中實作的 lifetime），di 層的第二道檢查（`provider cycle`、`singleton captures operation`）同樣生效。
- **Resolver/Scope 同規則**：`LifecycleResolver(owner)`（Start/Stop）與 `module.OpenScope` 共用同一張 owner-private 映射——owner 以 canonical `module.Contract[介面]()` 解析即轉入自家 alias；外部模組（含直接相依者）一律不可解析該介面，alias 識別本身也永不可作為 token。未加 scope 解析 Operation-lifetime alias 維持原本的 `operation provider requires scope` 錯誤。
- **別名供應者語意**：container 內 alias 是 delegation provider（`DependsOn: [選中實作]`、lifetime 同選中實作、passthrough factory），constructor 參數解析由包裝的 guarded resolver 換成 alias token——依賴守衛、迴圈、lifetime、context 取消與 cleanup 歸屬全部沿用 di 既有機制，不經過任何全域未守衛 resolver。
- 驗證：`go test ./kernel/module -run TestPrivate`（同模組 happy、直接相依不可見、兩模組共用介面獨立、missing/ambiguous/rebind、反序註冊、公開 owner 不可被本地匹配繞過、StartRoot/Stop/OpenScope 私有解析、週期/laundering/清理、失敗圖 factory 計數為 0）。

## Root 模式的 Operation Scope（root_scope.go）

受限 lifecycle resolver 故意不開關任何暫時性 scope；因此 `Operation()` 生命週期的服務在 Start／Stop 內透過 `Runtime.Services()` 完全取不到（Resolve 一律錯誤），而 Root 入口不會把 sealed container 交給模組。`Runtime.OpenScope(ctx)` 是推薦入口；`module.OpenScope(ctx, rt.Services())` 保留為 package-level 過渡 API，實作見 [root_scope.go](root_scope.go)：

```go
func (m *ReportModule) Start(ctx context.Context, rt module.Runtime) error {
    scope, err := rt.OpenScope(ctx)
    if err != nil {
        return err
    }
    defer scope.Close(ctx) // Close 擁有底下 scope 的全部清理（有界、冪等）
    session, err := module.Resolve[*ReportSession](ctx, scope) // Operation() 服務在此可解析
    ...
}
```

- **只接受受限 resolver**：nil context、nil resolver、typed-nil、自訂或委派包裝的 `di.Resolver`（含 raw sealed container、非本 engine 的實作）一律錯誤；實作利用同 package 對未導出 `lifecycleResolver` 的可視性做型別判斷，不重新實作組合引擎。container／ctx 失敗（closed container、canceled context）原樣向上冒泡。
- **不可變的可視性**：回傳未導出的 wrapper（`di.Scope` 介面）。每次 Resolve 檢查與 LifecycleResolver 完全相同的可視性（自身註冊——含 owner-private 介面 alias——+ 直接相依模組且已 Export 的 token），owner 在建立時由該 resolver 綁定、之後不可切換；wrapper 不暴露 container、組合引擎 handle，也不被型別斷言為 `di.Container`。
- **底下 scope 既有語意**：OpenScope 內部呼叫 container 自己的 `OpenScope`；同一 resolver 可開多個獨立 scope，各 scope 內的 Operation 同一實例、跨 scope 不同實例、cleanup 各自恰好一次；Close 冪等，ctx cancel／closure（包含開啟後取消）由既有 di scope 處理。
- **不變的行為**：一般 lifecycle Resolve 對 Operation 服務仍然拒絕（必須有顯式 scope）；建構子形狀與 lifetime 檢查全部不變（services.go／loader／runtime 無任何簽名或語意變更）。僅供 Root 模式；legacy `module.Module` 註冊路徑不產生此 resolver，也不需要此 API。
- 驗證：`go test ./kernel/module -run TestScope`（identity 同／跨 scope、一次工廠的併發 barrier 測試、private／非直接相依／unsupported resolver 拒絕、直接契約與中立取代、cancel 與 closed-container 清理）。`-race` 依 Gate 環境。
