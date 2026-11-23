# Queue、容量與停機操作手冊

部署必填 worker batch／concurrency、payload 上限、lease、execution／cleanup timeout、重試上限、backoff／jitter、retention 與總容量。Job Lease 大於 execution 加 cleanup；Handler 必須遵守取消並以 Job ID 冪等。事件使用 `NewBoundedStore` 並部署 `CapacityMigrations`，Jobs 部署自身 Migrations；遷移 role 與 app／worker role 分離。容量包含尚在保留窗口的終態紀錄，應按日常流量與保留期估算。

| 訊號 | 立即處置 | 恢復與對帳 |
|---|---|---|
| DB pool 飽和 | 停止擴大認領、降低並行，檢查長 UOW／未關 Rows；不要增加無界 goroutine | 以 acquire 等待、active／max、p95／p99 判斷；確認交易及連線釋放後逐步恢復 |
| Broker 離線／Outbox lag | API 在容量內仍可提交到 Outbox；固定 SendTimeout 與退避，停止超限接單，對外 429／Retry-After | 恢復 Broker 路由與持久化確認，先處理 pending；dead 由目前 owner 授權、必要 Audit 的 replay 重送，核對原 Event ID／owner／結果 |
| Inbox dead-letter | 隔離不相容 schema、無效 owner 或失效 grant；不回退成 tenant | 修復版本／授權後由同 owner replay，完整 envelope 不改；以 processed 與業務結果在獨立連線對帳 |
| Job dead／取消 | 保留安全錯誤碼、attempt、Job ID；檢查 deadline、schema、服務 grant 與代理使用者資格 | 在原 owner 重新授權及 Audit 後 replay；期限已過則先對帳再建立新操作，外部副作用沿用既有冪等憑據 |
| 容量達上限 | 不刪除仍受保證的資料、不在記憶體累積待處理清單；拒絕新工作並退避 | 依 retention 清除成功終態；dead 完成稽核封存與 reconciliation 後由明確維運程序清理，必要時評估磁碟及提高受控容量 |
| Migration 卡住 | 停止新部署，檢查全域 deployment lock／DB 活躍 session，勿同時啟動第二套工具 | 確認持鎖工作、保留故障 logs；以相同來源向前重跑，不改寫已套用 migration、不強行 allow-missing |
| Tenant／Scope 誤配置 | 停止受影響接單，核對 membership、scope provider、resource catalog／RLS 與 role | 使用獨立管理路徑修復並 Audit；不得停用 RLS、擅自改 owner 或以舊 allow cache 繞過撤權 |

Readiness 先撤回，再停止 HTTP 接入與 Worker 認領；Drain 已接入工作，期限到傳遞取消並等待有界 cleanup。只有 Handler 已退出才用有效 token 交還 lease；之後反序 Stop，DB／Broker 最後關閉。若 Handler 不遵守期限，保留故障並以 crash／lease 到期復原，不宣稱正常停機成功。

監測定期讀取 Outbox／Inbox／Jobs Stats，記錄 count、oldest age、dead、retry 與 pool saturation；`Telemetry.Queue` 以部署閾值告警。Metrics 不放 tenant／org／principal label；詳查使用帶安全 ID 的 logs／trace／必要 Audit。Sampling 與 trace／log retention 在應用 SDK／collector 部署中明示，Telemetry 故障不改寫已確認 Commit。

演練在專用環境停止 Broker 至工作達重試／容量上限，再恢復、授權 replay、排空並核對無遺失及 owner 不變。保存 lag／pool／heap／retry／drain 曲線；以實際停機時間至最後對帳完成量測 RTO。備份須包含 DB、schema 與可靠 Broker 狀態，以還原後獨立對帳量測 RPO／RTO，未測得生產還原結果前不承諾零損失。
