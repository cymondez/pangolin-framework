# Observability

`New(tracerProvider, meterProvider, writer, module)` 注入 OpenTelemetry API Provider 與 JSON log writer。框架不建立全域 Provider；應用負責 SDK／exporter、sampling、retention 與最後的有界 Shutdown。參考 [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/instrumentation/)。

`Telemetry.HTTP(handler)` 建立 request operation ID、還原 W3C traceparent、記錄 HTTP duration／result。`Telemetry.Context(ctx)` 將同一組 instrumentation 明確傳遞至 UOW／Outbox／Consumer；`Start(ctx, "use_case")` 可包住應用服務。只使用有限的 operation vocabulary，Metrics 不包含 Tenant／Scope／Principal／Event ID。Tenant／Scope 與權威 revision 僅出現在安全 ID 的 Trace attributes 和 JSON log。SQL 使用 `pgxpool.Config.ConnConfig.Tracer = telemetry`，只記錄操作結果，不記錄 SQL、args、DSN 或錯誤原文。

`TraceParent`／`Restore` 只傳遞 W3C trace context，不傳遞任意 baggage 或身分。Integration Event 的可省略 traceparent 與 Job 的 TraceParent 保留跨程序追蹤；CorrelationID／OperationID 協助對帳，完全不作授權。

`Queue` 記錄 outbox／inbox／jobs 聚合值與有限告警碼，超過 count／oldest 或出現 dead 發出 QUEUE_PRESSURE。呼叫端定期取得 queue Stats 後送入；告警不能改寫已確認交易結果。JSON log schema 不接受任意 message／payload／error 欄位，避免敏感資訊外洩。

`Health{Ready, Required, Timeout}` 提供 `/livez`、`/readyz`。存活不查外部服務；Ready 必須同時滿足 Host Ready 與必要 DB／模組檢查。Broker 僅在該程序確實需要它才能接單時列入 Required；純 API 的可選 Broker 故障透過 lag 告警處理。依賴檢查必須遵守 context。

錯誤設定拒絕組裝；SDK／writer 不由此 package 關閉。`go test ./observability` 驗證 health、redaction、metrics 與 propagation；`go test -tags=integration ./tests/m4` 驗證真實 API→SQL→Outbox→NATS→Consumer 關聯。
