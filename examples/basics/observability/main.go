package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"time"

	"github.com/pangolin-framework/pangolin/observability"
	"go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	// 步驟 1：由應用自己明確建立 OpenTelemetry TracerProvider 與 MeterProvider
	// （panGOlin 不偷建全域 Provider；正式部署請接入對應 Exporter 並調整取樣率）。
	traces := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1))))
	meters := metric.NewMeterProvider()
	defer func() {
		// 結束前使用獨立有界 context 關閉 SDK，確保緩衝中的追蹤資料能安全刷出。
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = traces.Shutdown(ctx)
		_ = meters.Shutdown(ctx)
	}()

	// 步驟 2：組裝 Telemetry 物件，傳入自訂的日誌輸出（這裡用記憶體 buffer 方便驗證 JSON log）。
	var logs bytes.Buffer
	telemetry, err := observability.New(traces, meters, &logs, "my-app")
	if err != nil {
		panic(err)
	}

	// 步驟 3：為操作建立帶 OperationID 與 Telemetry 的 context，開啟 "use_case" Span，
	// 並示範將 W3C traceparent 傳遞到跨程序／非同步情境後仍能還原出同一個 TraceID。
	ctx := telemetry.Context(observability.WithOperation(context.Background(), "demo-operation"))
	ctx, done := observability.Start(ctx, "use_case")
	continued := observability.Restore(context.Background(), observability.TraceParent(ctx))
	same := trace.SpanContextFromContext(ctx).TraceID() == trace.SpanContextFromContext(continued).TraceID()
	done(nil)
	fmt.Println("Trace correlated:", same)

	// 步驟 4：將標準 Health 檢查掛進 telemetry.HTTP，驗證 /livez 與 /readyz 皆回應 204 No Content。
	health := observability.Health{Ready: func() bool { return true }, Timeout: time.Second}
	for _, path := range []string{"/livez", "/readyz"} {
		result := httptest.NewRecorder()
		telemetry.HTTP(health).ServeHTTP(result, httptest.NewRequest("GET", path, nil))
		fmt.Printf("%s: %d\n", path, result.Code)
	}

	// 步驟 5：確認結構化 JSON 日誌中確實已關聯本次的 demo-operation 識別碼。
	fmt.Println("JSON log recorded:", bytes.Contains(logs.Bytes(), []byte("demo-operation")))
}
