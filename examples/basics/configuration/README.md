# 設定讀取

從 `examples/basics` 執行 `go run ./configuration`，預設輸出 `Application: My App`。設定 `APP_NAME=Bookstore` 後輸出 `Application: Bookstore`。明示空值會因必要設定缺漏而失敗。

範例以 `os.LookupEnv` 區分未設定與空值，將使用者環境明確映射成 Config Source。不輸出 Secrets。
