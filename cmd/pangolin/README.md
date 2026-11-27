# pangolin 0.1.0

從倉庫根目錄 `go build -o .cache/bin/pangolin.exe ./cmd/pangolin`，使用 `pangolin --help`。入口建立可取消 context，將參數及工作目錄交給 internal/scaffold；錯誤非零退出。所有資源由命令實作回收；Ctrl+C 取消外部工具及資料庫操作。

目前提供 modular-monolith 的 tenant-only／organization-enabled 範本、module add、migrate create/status、doctor、generate openapi。產品版本固定 0.1.0。模組 CLI 與生成物測試見 internal/scaffold README、M5 開發指南；Windows／Linux 使用相同參數。

## 即時通訊選項

`pangolin new NAME --template modular-monolith --realtime` 為生成專案增加 SignalR 入口。可與 `--with organization` 合用。設定與發布路由見生成專案的 `REALTIME.md`。
