# ADR13：首版 CLI 的同版 Runtime Bundle

日期：2026-11-23。狀態：Accepted。產品版本固定 0.1.0。

M5 要求 clean runner 能直接生成、建置及執行兩種 Bookstore，但首版 v0.1.0 tag 尚未通過 M6 發行。生成不存在的遠端依賴、要求使用者事後修補 replace 或從開發機偷偷 import examples，都無法構成可重現生成物。

CLI 將 production packages、SQL、模組 README、go.mod/sum 與 VERSION 封裝成純文字模板，生成專案內獨立 framework module，以固定 v0.1.0 require 及相對 replace 使用。scripts/template_bundle.py 從同一 checkout 再生成，Gate 核對原始 bytes；生成物的 runtime-manifest.json 保存實際輸出檔案的 SHA-256。tenant-only 不輸出 organization implementation，go list 再確認沒有反向相依。

CLI 自身仍列入手寫 production coverage。Bundle 純文字不重複灌入分母；生成物另在兩平台測試並連真實 PostgreSQL／IdP，host 另跑兩種 Compose。Local IdP 明示為 fixture，不能混入正式 framework API 或聲稱為 production identity provider。

代價是生成專案較大，更新 Runtime 必須同步來源與 manifest。首次正式發行後可明示遷移到同版遠端依賴；不在 M5 假設尚未存在的發布 tag。這不新增版本或放寬安全／交易契約，也不授權既有資料無遷移切換 Profile。
