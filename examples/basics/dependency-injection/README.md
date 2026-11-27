# 依賴注入（進階相容：獨立 DI 容器）

從 `examples/basics` 執行 `go run ./dependency-injection`，輸出 `Hello, Alice!`。不需要外部服務。

本例是進階相容路徑：不經 root 組合，直接使用 `kernel/di` 的 Registry／Container。以應用定義的 Greeter 介面作為 Key 的型別，註冊 EnglishGreeter Singleton；封存後解析並呼叫，最後用有界 context 關閉 Container，保留清理錯誤。本例無外部資源；連線池等 Factory 必須回傳自己的 Cleanup。Root 裝配的預設做法見[依賴注入手冊](../../../docs/site/content/fundamentals/dependency-injection.mdx)與 `root-greeting` 範例。
