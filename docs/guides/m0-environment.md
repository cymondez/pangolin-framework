# M0 本機與 CI 環境

版本見 `deploy/toolchain.json`；Go／PostgreSQL／NATS／Toxiproxy 映像以公開 registry index digest 固定。PostgreSQL 18+ volume 掛載 `/var/lib/postgresql`。資料、容器與 network 皆由本專案自己的 Compose namespace 管理。

## 操作前盤點

讀取現有容器、Compose project、ports／volumes、可用記憶體與 CPU。若 `pangolin-m0` 已存在，確認是自己的工作環境才採用；其他 checkout 改用獨立 project 名稱及不同 ports。不能因為連不上 daemon 就擅自啟動、重啟、重設 Docker Desktop／Engine 或 WSL。

2026-11-21 本次盤點發現其他專案使用 5432、53、80、443、1025，因此本專案不使用那些 ports。這是當次觀察，不是未來免檢查的保證。

| 服務 | loopback port | 記憶體上限 | CPU 上限 |
|---|---|---|---|
| PostgreSQL | 15432 | 256 MiB | 0.5 |
| NATS JetStream | 14222 | 128 MiB | 0.5 |
| Toxiproxy API／PG／NATS | 18474／15433／14223 | 64 MiB | 0.5 |
| Test IdP | 18080 | 64 MiB | 0.5 |

可透過 `PANGOLIN_PG_PORT`、`PANGOLIN_NATS_PORT`、`PANGOLIN_TOXI_PORT`、`PANGOLIN_PG_PROXY_PORT`、`PANGOLIN_NATS_PROXY_PORT`、`PANGOLIN_IDP_PORT` 選擇其他未占用 port。`smoke.py` 對 NATS／IdP／Toxiproxy 讀取相同環境變數。Compose `-p` 與 smoke 的 `--project` 必須一致。

## 建立與驗證

在 repository root 執行 README 的 build／config／up／smoke 流程。Windows 使用 `python`，不使用 WindowsApps 的 python3 alias。Linux 可用對應 Python 3 interpreter。build 腳本只在自身程序暫時設定 GOOS／GOARCH／CGO，結束即還原。

`smoke.py` 驗證：四個服務屬於預期 project；PG 版本／基本 rollback／app role 不具 superuser／BYPASSRLS／PG proxy；JetStream file stream 的 publish ack／durable consume／ACK；測試 IdP 的 discovery／JWKS／實際 RSA 簽章；本專案 NATS proxy 的故障與還原。它不證明正式 UOW、RLS、Outbox／Inbox 或 JWT Validator 已交付。

結果位於 `artifacts/m0-smoke.json`，包含來源 commit、working tree 是否有修改、來源樹 hash 及各項結果。工作樹尚未 commit 的證據不可當成正式同 commit 的 Verified。CI 在 disposable runner 使用 `pangolin-m0-ci`；會清理自己建立的容器，不刪除 volume、不做全域 prune。

M0 最終驗收另使用 `pangolin-m0-<commit-short>` 及六個經盤點未占用的 ports，預先確認同名 volume 不存在，以空資料庫／空 JetStream store 重建。Compose 的 `PANGOLIN_SOURCE_COMMIT` label 記錄來源版本；選定 port 的 env 檔保存在該 commit 的 environment 證據目錄。`m0.py verify --environment` 保存 smoke、Compose log、Go／平台 metadata、配置及 log hash；Windows／Linux／environment 三組證據交由 `m0.py gate` 核對。

## 故障與結束工作

proxy 的 `postgres`／`nats` 名稱只屬於這個測試專案。手動注入故障前先確認 upstream；結束後恢復原狀。`smoke.py` 遇到原本已停用的 proxy 會拒絕覆寫別人的故障場景。

停止本次建立的環境時，先確認 project 與容器歸屬，再使用帶明確 `-p`／`-f` 的 Compose 命令。不要刪除他人容器、volume、network 或全域 cache。保留本專案資料不等於完成備份／復原演練。
