# 本地 Tenant 權威投影

`Authority{Pool}.Check(ctx, principal, tenant)` 只讀取 `pangolin_security.tenant_memberships` 目前 active 的單一資格與 revision；user／service 各自具有精確 key，查無資料拒絕，來源錯誤保留 cause 並回 `SCOPE_AUTHORITY_UNAVAILABLE`。業務 Repository 不使用此 Pool；這是專用控制面查驗 Port。

部署時透過 `Migrations` 的 `migrations` 子目錄註冊 Goose。外部 Identity 同步必須原子更新本地 state／revision，確認後才回覆本地治理成功。一般 app／worker 只有 SELECT，資格管理由可信控制面執行。Adapter 不自行關 Pool，由組裝擁有者負責停機。

`go test ./tenancy/...` 及 `go test -tags=integration ./tests/m3`；完整 Job Worker 屬後續交付，這裡提供其重新授權所需的共用契約。
