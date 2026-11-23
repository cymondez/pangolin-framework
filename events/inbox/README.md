# Inbox 接收與處理

`NewReceiver(store, registry, group, verify, alert)` 要求獨立的 Broker session／route 驗證；`Receive(ctx, envelope, ack)` 先保存完整 Envelope、驗證後 producer 與 fingerprint，持久化成功或確認同內容重複後才 ACK。相同 group／Event ID 不同內容回 `INBOX_CONTENT_CONFLICT` 並告警，不覆寫原事件。

`NewWorker` 注入 UOW Manager、目前服務身分的 bind、Handler 與有界 retry。候選枚舉不授權也不認領；先重新 Bind／admission，然後同一可寫 UOW 內 `FOR UPDATE SKIP LOCKED`、鎖內重新核對 fingerprint／version／owner，執行 Handler 並標 processed。行鎖直到 Commit，崩潰回滾後可再認領。確定回滾後 retry 更新需匹配版本，不能覆蓋已完成結果；Commit Unknown 不更新為確定失敗。缺少 Provider／授權來源故障或 Scope 不符時拒絕處理並記錄告警，不降級為 Tenant，也不修改已保存的 Envelope。

外部 API 不在本地交易內：`ExternalKey(group,event,operation)` 提供跨重試穩定鍵，接收端必須真正支援冪等；否則透過本地 Outbox 派送，不宣稱 exactly-once。Store／Manager／Broker 生命週期由組裝端擁有。去重保留期須涵蓋 Broker／人工重播窗口，超出不宣稱防重。驗證：`go test -tags=integration ./tests/m3`。

對可信持久化候選，Bind／binding 驗證或 root admission 拒絕也會以原 fingerprint／version 有界延期並累計 dead 門檻，避免永遠堵住同群組較新的合法事件。重試 metadata 使用獨立有界清理 context；Unknown／已 processed 或較新版本不被重寫。
