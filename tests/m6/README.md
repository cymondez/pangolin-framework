# tests/m6 — 發布 Gate 回歸

本目錄持有 M6（WP18）發布 Gate 的 Python 回歸 `test_gate.py`，以及 A72（REL-005）可執行演練的 Go 整合測試（`drills_test.go`／`helper_test.go`，屬發布演練工作包）。Python 回歸對應 `tests/contract/m6.json` 的 8 個 A70／A95 subtest；A72 的 5 個 subtest 由 `scripts/m6_drills.py` 產生的 `release-drills.json` 演練證據覆蓋。

## 執行方式

從 repository root：

```powershell
python -m unittest discover -v -s tests/m6 -p test_*.py
go test -tags=integration ./tests/m6   # 需要 PANGOLIN_M1_* fixture DSN（drills 測試）
```

Go 演練測試使用與舊里程碑相同的 `PANGOLIN_M1_ADMIN_DSN`／`APP_DSN`／`WORKER_DSN`／`NATS_URL`／`ISSUER`，並以 `PANGOLIN_M6_EVIDENCE_DIR` 輸出機器可讀證據供 `scripts/m6_drills.py` 驗證；每個演練自建且自清專屬資料庫，不觸碰共享 fixture 資料。

## 覆蓋的拒絕語意

`test_gate.py` 以真實倉庫資料與暫存副本做行為測試，不鏡射實作、不偽造整合成功：

- **A70/missing-release-item**：移除任一必交項（資產、交付、演練、Profile 列、schema、Gate）即發布失敗並列出缺項；缺檔資產逐一列名。
- **A70/unknown-requirement**：未知需求 ID、未知或跨里程碑錯置的 subtest 一律拒絕。
- **A70/duplicate-delivery**：同一案例被兩個需求／里程碑宣稱交付即拒絕。
- **A70/template-runtime-version**：runtime／CLI／Template 版本漂移即拒絕；wire schema 版本獨立於產品版本（version 1 搭配 0.1.0 為合法綁定）。
- **A95/organization-contract-fails**：必要組織案例未出現在報告時，發布閉包檢查以缺項拒絕。
- **A95/applicable-case-skipped**：Python 回歸 skip、演練 skipped、適用 subtest 未被映射均拒絕。
- **A95/critical-package-below-threshold**：coverage 以原始計數精確比較，89.99% 失敗、90.00% 通過，不得以整體平均掩蓋關鍵 package。
- **A95/tenant-only-build-independent**：tenant-only 組裝的 `go list -deps` 不得依賴 optional organization 模組。

## release-drills.json 介面

`scripts/m6_drills.py --output <dir> --project <project>` 必須產出 `<dir>/release-drills.json`（完整契約見 `.workflow/m6/results/release-drills.md`）。本 Gate 的 `validate_drills` 在其上獨立驗證：

- **Provenance**：`commit`、`source_sha256`（m1.py 來源 bytes 方法）與驗證 run 相同；乾淨 commit 要求 `working_tree_dirty=false` 且 `evidence_class="evidence-ready"`；`platform` 為 `GOOS`/`GOARCH` 物件，inline 演練必須等於驗證平台；`project` 符合 `pangolin-m6-[a-z0-9-]+`；`baseline` v1.3、`product_version` 0.1.0。
- **命令與資產 hash**：頂層與每個 drill 的 `commands`（`argv`、`exit_code` 0、log 與 sha256）逐條驗證 log digest；`artifacts` 映射內每個證據檔的 sha256 重新計算比對。
- **五項 drill**：`drills` 恰含 `clean-rebuild`、`forward-upgrade`、`backup-restore`、`load-baseline`、`security-review`，各 `status: "passed"`、非空 `commands` 與非空 `evidence`（演練腳本自行再驗證的檔案內容／hash）。
- **逐項 release-blocking `checks`**（每個 drill 條目另附的扁平斷言，由本 Gate 獨立檢查）：升級 `seeded_tenant_rows>=1`、`owner_preserved`、`force_rls` 且無 permissive policy 殘留；備份復原 `restore_verified`、`row_counts_match`、`checksums_match`、`independent_connection`；負載含硬體／pool／load／concurrency／p95／p99／methodology 與至少 10 個原始樣本；安全審查覆蓋規範 §14.1 全部 12 類、`unresolved_high_risk=0` 且每類有 attack surface／abuse path／control／executable test／residual risk。

Linux 固定 runner 允許 `PANGOLIN_M6_DRILLS_DIR` 指向 host 演練結果（`artifacts/` 內、含 `release-drills.json`），複製前後 provenance 與每個檔案 hash 都會重驗；來源 bytes 不符即拒絕。dirty 開發 run 由演練腳本自行偵測並標記 `development-only`，僅供除錯；Gate 只接受 evidence-ready 證據。

## 交付邊界

本目錄不宣稱 M6 Verified；`release-drills.json`、runbook 與安全審查文件由發布演練工作包交付。正式狀態由里程碑規劃書依同 commit 雙平台 Gate 證據維護。
