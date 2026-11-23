# Config：四層啟動設定

`NewLoader(rules)` 依名稱合併 `defaults < file < environment < cli`，與傳入 Source 的排列順序無關。Source.Values 由組裝端提供；Loader 不會自行讀取任意檔案、掃描環境變數或解析 flags。契約見 [contracts.go](contracts.go)，實作見 [loader.go](loader.go)。

```go
reader, err := config.NewLoader([]config.Rule{
    {Key: "database.dsn", Required: true, Validate: config.URL("postgres", "postgresql")},
}).Load([]config.Source{
    {Name: "defaults", Values: map[string]string{"service.name": "bookstore"}},
    {Name: "environment", Values: map[string]string{"database.dsn": os.Getenv("PANGOLIN_DSN")}},
})
// 檢查 err 後，透過 reader.Lookup(key) 讀取。
```

同名 Source、未知 Source、空 key、缺少必要值與驗證失敗均回傳 error。Loader 複製 Values；封存 Reader 沒有 setter，原 map 後續修改不影響執行中的設定。驗證錯誤只記錄 key，不串接可能包含 DSN／Token 的 value 或 validator 錯誤內容。

Runtime 使用 `StartupRules()` 要求 `uow.default_timeout`、`uow.cleanup_timeout`、`uow.hook_timeout` 為正 duration，以及 `uow.max_requires_new_depth` 為正整數。Adapter 必須另宣告自己的必要欄位；Bookstore 的實際組裝見 [bootstrap](../../examples/bookstore/bootstrap/README.md)。Secrets 只從可信環境／密鑰來源注入。

驗證：`go test ./kernel/config ./kernel/runtime`。A47 涵蓋四層優先序、必要值、封存與敏感值遮罩。

`ParseJSON(bytes)` 在 Configuration 層產生不可變 Reader：冒號階層、零起算十進位陣列 index（含多位數／巢狀陣列）、點號 literal、OrdinalIgnoreCase 比對與重複攤平 key 拒絕。依 .NET 10 JSON Configuration，null／空物件沒有字串值，空陣列為空字串，bool 為 True／False，number 保留原文字；支援 comments／trailing comma 與深度 64。空 container 最後賦值覆蓋既有值，後續 scalar 重複則拒絕，與原解析器一致。它不改寫既有 Source／Loader API；組裝者可直接把 Reader 注入 Settings Configuration Provider。測試由真實 JSON 輸入開始。

對照來源：[.NET 10 JSON Configuration parser](https://github.com/dotnet/runtime/blob/v10.0.0/src/libraries/Microsoft.Extensions.Configuration.Json/src/JsonConfigurationFileParser.cs)。
