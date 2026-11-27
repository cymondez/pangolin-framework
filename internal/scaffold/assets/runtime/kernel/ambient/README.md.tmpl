# kernel/ambient：顯式不可變 Ambient Scope 原語

`kernel/ambient` 提供 `ambient.Var[T]`，作為 panGOlin-framework 跨層傳遞請求／操作環境狀態（例如 Identity、Tenant、ScopeBinding、Unit of Work 視圖、資料庫執行器、事件佇列與 Telemetry）的統一底層原語。本套件只依賴 Go 標準函式庫 `context`，零框架內部依賴。

## 設計原則

- **顯式傳遞、零 GLS**：所有狀態依附於顯式傳入的 `context.Context`，不依賴 goroutine-local storage（GLS）、全域變數、執行期棧檢查或反射攔截器。
- **不可變 Copy-on-Write 巢狀鏈**：每個 `With`／`Clear` 都會衍生新的不可變 `frame[T]` 節點，絕不回寫父節點，也沒有共享可變計數器。這與非同步執行流中常見的 Copy-on-Write 環境隔離模型（如 `AsyncLocal<T>`）在結構上同構：多個 goroutine 可同時從同一個父 `context.Context` 衍生出各自獨立的子範圍（例如平行 `uow.RequiresNew`），互不鎖死、互不汙染。
- **套件私有防偽造**：每次呼叫 `ambient.New[T]()` 都會配置獨立且非零大小的未匯出指標鍵。各框架套件把自己的 `Var[T]` 宣告為 package-private，只透過受控公開 API 讀寫，確保外部程式碼無法憑字串鍵偽造 Tenant、Principal、ScopeBinding 或 UoW 狀態。

## 用法

```go
// 在擁有者套件內部宣告為未匯出變數，守住安全邊界。
var tenantVar = ambient.New[string]()

func WithTenant(ctx context.Context, tenant string) context.Context {
    return tenantVar.With(ctx, tenant)
}

func CurrentTenant(ctx context.Context) (string, bool) {
    return tenantVar.Current(ctx)
}

// 暫時覆寫並自動還原（父 ctx 不可變）：
err := tenantVar.Scope(ctx, "tenant-b", func(inner context.Context) error {
    cur, _ := tenantVar.Current(inner)   // "tenant-b"
    outer, _ := tenantVar.Outer(inner)   // 外層值
    depth := tenantVar.Depth(inner)      // 巢狀深度
    _, _, _ = cur, outer, depth
    return nil
})

// 清除槽位（例如脫離目前 UoW 並保留其餘身分與期限）：
detached := tenantVar.Clear(ctx)
```

`ambient.Slot` 是僅由 `*Var[T]` 實作的封閉介面，供生命週期邊界（如 `uow.Detach`）清除已掛載的資源槽位，而不開放任意 context 改寫回呼。

驗證：`go test ./kernel/ambient`。
