# ADR03 — UOW 契約與本機配置

狀態：採用於 M0；Runtime 待實作。對應 ENV05、UOW-001–032。

原樣採納規範 §5 的 `Within`／`Current`、Options、EffectiveOptions、Scope、Completion；不增加業務端 Begin／Commit handle。Required 共用根責任，RequiresNew 暫停父視圖並使用獨立資源；錯誤、Unknown、Panic、清理及回呼依規範，不由 Adapter 自選。

可機讀數值固定於 `deploy/profiles/local.json`：DefaultTimeout 5s、CleanupTimeout 2s、HookTimeout 1s、MaxRequiresNewDepth 4、MaxEventsPerUOW 256、MaxDispatchDepth 16。這是本機／CI 起始 profile，非效能承諾；production profile 必須明示全部必要值。

fixture 先覆蓋選項矩陣、子視圖失效、rollback-only、父子不同 transaction ID、同 Executor busy、取消、Commit Unknown 與 callback 錯誤隔離。A01–A95 清單中的 UOW 案例目前全部 planned。

M2 實作依既定 §5 契約擴充；新增 Adapter 可選 DatabaseKey／RequiresTransaction 與 Flush／BeforeCommit／Prepare／Release 完成階段，仍由 Manager 獨占責任。這些組裝擴充不授權業務端提交。變更與遷移見 [M2 說明](../release-notes/m2.md)。實際完成狀態由里程碑與同版 evidence 維護，原 M0 決策與歷史狀態保留。
