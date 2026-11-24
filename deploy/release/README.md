# deploy/release — v0.1.0 發布清單

本目錄持有 M6（WP18）的**發布清單（release manifest）**：`manifest.json` 以宣告方式定義 v0.1.0 發布必須綁定的內容，包括規範基線與版本範圍、95 項需求的唯一交付映射、421 個具名 subtest、三組 Profile 適用性列、runtime／CLI／Template 版本綁定、已發布 Event Schema、coverage 關鍵 package、必備 release 資產與五項發布演練。清單本身不宣稱任何驗收成功；所有宣告都由 `scripts/release_manifest.py` 對權威來源（實作規範、里程碑規劃 §5 交付表、`tests/contract/*`、Go 原始碼與生成物）重新驗證，任何缺項、未知或重複交付、版本漂移都會 fail-closed 阻擋發布。

## 驗證方式

- 靜態檢查（不含執行演練）：`python scripts/release_manifest.py --check`。輸出摘要；缺項以非零退出碼列出。
- 完整發布驗收：`python scripts/m6.py verify --project pangolin-m6-dev`，同時執行 M0–M5 累積 Gate、五項演練（`scripts/m6_drills.py`）與機器產生的 `release-manifest.json` 證據（綁定 commit、來源 bytes、coverage 原始計數與演練 hash）。
- 雙平台 Gate：`python scripts/m6.py gate --commit <完整 SHA>`。

## 版本與 Schema 政策

runtime（`VERSION`）、CLI（`internal/scaffold/cli.go` 的 `Version` 常數）與 Template（`internal/scaffold/assets` 的同版 runtime bundle 及其 `require github.com/cymondez/pangolin-framework v0.1.0`）都綁定 0.1.0；發布驗收要求三者一致。已發布 Event Schema 使用自己的 wire 版本（v0.1.0 發布 `bookstore.book_created` 與 organization 系列事件的 schema version 1），**不要求** schema 版號等於產品版本；清單逐一綁定 schema 的來源檔案，防止虛構或未宣告的 schema。

## 錯誤與清理行為

`manifest.json` 是宣告式文件，不寫入執行結果；發布證據由 Gate 產生在 `artifacts/<commit>/m6/<platform>/release-manifest.json`。本目錄不管理任何服務或外部資源，無清理責任。

## 目前交付邊界

- M6 尚未通過同版雙平台 Gate；`docs/runbooks/release.md`、`docs/security/release-review.md`、`docs/release-notes/0.1.0.md` 等清單要求的資產由發布演練工作包交付，缺檔時本檢查 fail-closed。
- 五項演練由 `scripts/m6_drills.py` 執行（另一工作包）；其 `release-drills.json` 介面由 `scripts/release_manifest.py` 的 `validate_drills` 驗證：同 commit／來源 bytes provenance、真實命令 log 與 hash、逐項實測欄位（升級的 FORCE RLS 與 owner 保留、備份復原的獨立連線對帳、負載的 p95／p99 與原始樣本、安全審查覆蓋 §14.1 全部 12 類威脅且無未解高風險）。
- 本清單不取代實作規範 §13／§15 或里程碑規劃 §5；正式 Verified 狀態只由同 commit Gate 證據更新。
