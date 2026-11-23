# 觀測與 Health 範例

從 examples/basics 執行 `go run ./observability`，不需要外部服務。完整程式建立明確注入的 OpenTelemetry Provider、傳遞 W3C Trace Context、檢查 JSON log，以及 livez／readyz。預期輸出四行：Trace correlated: true、/livez: 204、/readyz: 204、JSON log recorded: true。

範例使用記憶體 writer，不開 listener；由程式以有界 context 關閉 SDK。正式應用自行提供 exporter、sampling 與 retention。以 `python scripts/docs_examples.py` 在獨立 consumer 專案執行全部範例。
