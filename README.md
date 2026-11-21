# panGOlin-framework

Go 模組化應用框架，採模組化單體、明確 DI、UOW、Tenant／Scope 隔離與可靠事件。產品目標 v0.1，規範基線 v1.3。

目前正在建立 **M0 工程底座**。Runtime、UOW、Repository、正式 Broker Adapter、CLI 與 Organization 模組尚未實作；綠色的 M0 檢查不代表 95 項產品需求已驗收。

## 本機檢查

使用 Go 1.27.4、Python 3.11+（M0 檢查只用標準庫），Windows PowerShell：

```powershell
./scripts/check.ps1
```

Linux（race 需要 C compiler／CGO）：

```sh
sh scripts/check.sh
```

`python scripts/m0.py check` 另檢查公開 API surface、421 個具名驗收計畫、三種 Profile 的適用性、fixture、import 邊界與 stdlib-only 相依基線。API 變更須先同步規範／ADR，再明示執行 `python scripts/m0.py freeze-api`；CI 不自動更新快照來放過漂移。

正式 M0 證據先固定為乾淨 commit，再執行 `python scripts/m0.py verify`；固定 Linux runner 加 `--race`，本專案空環境加 `--environment --project <name>`。三組結果保存於 `artifacts/<commit>/m0/`，最後以 `python scripts/m0.py gate --commit <full-sha>` 驗證相同版本、log hash、必要檢查與 Runtime 發布拒絕防護。這只判定 M0，不驗收後續 Runtime 功能。

檢查包含 build、vet、95 項需求／案例／交付映射、單元測試及整體／關鍵 package 90% statement coverage。`internal/testkit` 為逐檔列出的測試 fixture 排除項；尚未實作的 Runtime 案例只列在 manifest，不用假成功或 skip 通過。`go run ./tools/check -release` 目前應以非零結束，明示缺少 Runtime 驗收證據。

## 隔離的測試環境

詳見 [M0 環境指南](docs/guides/m0-environment.md)。操作前先盤點既有環境。預設 Compose project 為 `pangolin-m0`，不占用本機 5432／4222；服務皆綁 loopback，資源總上限 512 MiB／2 CPU。不要重啟共用 Docker Desktop 來處理本專案連線問題。

```powershell
./scripts/build-test-idp.ps1
docker compose -p pangolin-m0 -f deploy/compose/compose.yaml config --quiet
docker compose -p pangolin-m0 -f deploy/compose/compose.yaml up -d --build --wait --wait-timeout 90
python scripts/smoke.py
```

這些命令是環境驗證流程，需先確認名稱／port 沒有衝突。`smoke.py` 不啟停任何容器，只操作已確認屬於該專案的 DB 與測試 Broker／proxy，並還原自己的故障開關。Test IdP 是無驗證的本機 fixture，不能作正式身分供應商。

## 文件入口

- [代理導覽](AGENT.md)、[共用環境操作規則](AGENTS.md)
- [實作規範](design/panGOlin-framework_實作規範書.md)
- [里程碑、進度與估算](design/panGOlin-framework_里程碑規劃書.md)
- [M0 ADR](docs/adr/README.md)
- [驗收底稿](tests/contract/README.md)

尚未設定正式 release workflow；新增的 GitHub Actions 僅驗證 M0 工程底座，需 push 後才有遠端執行證據。
