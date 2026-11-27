# check：契約與 coverage Gate

`go run ./tools/check` 檢查 95 個需求、案例與唯一里程碑交付映射；`-coverage <atomic.out>` 從當前 build 的 go-list inventory 與逐檔 exclusions 計算整體／各關鍵 package 的 90% 門檻。完整 suite 合併由 `scripts/test_runner.py` 執行。

任何關鍵 package 不足 90% 都失敗，即使整體已達標。錯誤／缺漏 evidence 也回傳非零。`-release` 拒絕以靜態 inventory 直接認證發布，並指向 `python scripts/verify.py gate --commit <SHA>`：該入口驗證全部需求、三種 Profile、同版雙平台品質與發布演練證據。歷史拒絕測試保持有效，不能以一般 inventory 成功取代 Runtime 驗收。

驗證：`go test ./tools/check`，包含 overall 掩蓋 critical package 失敗的反例；來源實作與計數契約見 [internal/quality](../../internal/quality/README.md)。
