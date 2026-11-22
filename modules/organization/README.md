# organization

單一可選模組，封裝 Enterprise、Organization、Membership 與治理的授權來源。`NewModule(identity, authority)` 一次註冊 `org.enterprise` 與 `org.organization`，產品版本 0.1.0。Core 僅依賴中立 [Scope Port](../../security/scope/README.md)，tenant-only 不 import 本模組、不建立組織表或路由。

`Authority` 由受信任的組裝程式注入，以一致快照回答 Resolve／Authorize；不得使用 DTO 或 JWT 的組織聲明當作權威。Resolver 核對完整 owner、principal 及非空 revision 後才安裝 binding。Authority 故障應回傳 `scope.ErrAuthorityUnavailable` 並拒絕資料存取，無隱式 tenant fallback。Runtime 在任何 Start 前檢查 RequiredScopeKinds；重複 Kind／Registry Provider 導致啟動失敗。

M2 僅交付模組／Provider 邊界與兩 Org 整合薄片。完整治理 Management、預設 PostgreSQL Authority、migration、撤權與替換資料對帳留待 M3，不以此薄片宣稱完成。公開管理 DTO 位於 [contracts](contracts/README.md)。Provider 不取得可關閉資源，外部 Authority 的生命週期由其 DI owner 負責。

驗證：`go test ./modules/organization ./security/scope ./kernel/runtime`；真實資料隔離案例由 `tests/m2` 執行。
