# Review P2 — IntegrityReview（2026-11-26）

審查範圍：workflow task P2（`signalr-realtime-m7`）變更集：
`modules/realtime/contract_test.go`、`modules/realtime/signalr/contract_test.go`、
`tests/contract/m7.json`、`tests/contract/subtests.json`（A96–A104 追加）、
`.workflow/signalr-realtime-m7/results/p2.md`、`artifacts/m7-append-subtests.py`、`artifacts/m7purge.go`。

## Verdict

**PASS**（附 advisory 事項，無 blocking finding）

## 核對結果

### (a) 測試真實性
- `modules/realtime/contract_test.go` 與 `modules/realtime/signalr/contract_test.go` 的每個具名子測試均有實質斷言（錯誤碼以 `errors.Is` 比對 `scope.ErrDenied`、`uow.ErrRequired`、`ErrInvalid`、`ErrCapacity`、`ErrCredential`、`ErrMethod` 等真實 sentinel；負向情境用官方 client 不會生成的原始 frame／raw WebSocket／`negotiateVersion=0` 等輸入）。無 fake/silent-success 斷言、無 t.Skip 假成功。
- 使用之 helper（`testHub`、`openSession`、`subscribe`、`publishEvent`、`getDelivery`、`currentAuthority`）均定義於既有 `hub_test.go`；`tenantScope` 取自已審 production 的 `hub.go`（同 package 可用）；`signalr/contract_test.go` 自帶 `demoAuthority` fixture，屬合法測試替身（實作 `identity.Authority` Port），非取代真實相依。

### (b) 映射一致性（程式化核對，唯讀 python 檢查）
- `subtests.json` M7 子測試 61 項 ↔ `m7.json` unit(35)+template(17)+pending(9)＝61，雙向無缺漏；每個 unit 子測試的 `test` 欄位與 `m7.json` 逐字一致。
- m7.json 35 個 unit 映射指向的 `TestA96RT001`…`TestA103RT008` 與各 subtest 名稱全部存在於兩個 contract_test.go；realtime 7 個、signalr 2 個測試函式全部對上。
- `subtests.json` 追加 groups 共 9 個（A96–A104），requirement RT-001..RT-009、milestone M7；欄位慣例（`stimulus_variant`/`assertion_ref`/`evidence`/`test`/`template`）與既有 group 一致；groups 總數 104。`git diff --stat` 596 insertions, 0 deletions，且 diff 無 `^-` 刪除行——純追加確認。
- template_tests 17 項逐一核對 `examples/realtime/client/integration.test.mjs`：官方 client invoke／completion error／push、heartbeat（type 6 ping 斷言）、reconnect（`withAutomaticReconnect`＋手動 terminate）、enterprise-child 負向、multi-membership、reconnect-denied（撤權後重連被拒）、rollback → 兩節點無訊息（no-commit-no-success）、outbox committed/rollback、兩節點各收對應事件、三 Profile 迴圈即 generated-profiles／root-constructor／tenant-only／organization-enabled／replacement-contract——主張可定性支持，無虛構映射。
- pending 9 項理由具體（真實故障注入、節點生命週期、observability 共用套件、發布 Gate），未以 mock 冒充（符合 §13「Mock 不作為語意證據」）。

### (c) production 程式碼
- 工作樹確有 P2 以外的大量未提交 production 變更（events/nats、scaffold、spec §13 M7 矩陣＋§17、kernel/app 等），屬 P1／main owner 既有工作（`state.json` P1 in_progress, owner main），P2 record 未將其納入交付；P2 自身檔案全部為測試／契約／結果／一次性工具，無 production 修改。`artifacts/m7purge.go` 為 artifacts 根唯一 Go 檔，不與其他 main 衝突。
- `hub_test.go` 未經 git 追蹤（P1 產出）無法 diff；核得其 3 個測試函式與 helper 完整，與 packet 描述一致，無弱化跡象。

### (d) spec
- spec 的 M7 矩陣／§17 增補存在於工作樹，內容即 P2 情境的來源定義（RT-001–RT-009），屬前置（P1/主代理）工作；P2 交付檔案不含 spec。

### (e) JSON 慣例
- 兩份 JSON 均可解析；m7.json 鍵集（milestone/baseline/unit_tests/integration_tests/python_tests/template_tests/pending/thin_slices/module_docs）為 m5.json 子集，template_tests 用 m5 同款字串映射（`examples-realtime-official-client`）；evidence 路徑沿用 `artifacts/{commit}/{profile}/…` 慣例。

### (f) 既測試未動
- 見 (c)；兩個 contract_test.go 為新增檔，未觸及既有測試函式語意。

### (g) results/p2.md 與實況一致性
- 檔案清單、35/17/9/61 計數、596/0 diff 數字、pending 理由、mapping 表均與實況一致。唯 A97 小節計數描述不精確（見下方 Findings A-1）。

## Findings

### Advisory
- **A-1**（advisory）`.workflow/signalr-realtime-m7/results/p2.md`，A97 驗證列：「A97 五項（另含未註冊的 no-client-streaming 附加檢查）」。實際 `TestA97RT002` 有 6 個子測試：4 個已註冊（invalid-method/invalid-frame/oversize/cancel），另有 **兩個** 未註冊附加檢查——`invalid-arguments` 與 `no-client-streaming`，p2.md 僅提及後者、「五項」計數不對。建議改為「A97 註冊四項＋兩個未註冊附加檢查（invalid-arguments、no-client-streaming）」。
- **A-2**（advisory）`modules/realtime/signalr/contract_test.go`：`invalid-arguments`、`no-client-streaming` 兩個實際執行的子測試不在 spec RT-002 矩陣、m7.json、subtests.json 中。作為矩陣外的負向補充可接受（p2.md 已披露），但契約盤點不枚舉套件內全部執行的測試；建議在 M7 Gate 紀錄中註明，或為其補 named scenario（若規範有意納入）。
- **A-3**（advisory）`tests/contract/m7.json`：未含 m5.json／m6.json 皆有的 `regression_tests`／`regression_integration_tests` 鍵。grep `internal/quality/contract.go` 與 tools/check 無對該鍵的期待，現況無害；P4 累積回歸註冊時宜補齊鍵以維持慣例一致。

### 驗證範圍聲明
- 依 IntegrityReviewer 約束本次未重跑 build/test（不得執行驗證與修改）；靜態核對涵蓋：git status/diff、兩份 JSON 的程式化解析與映射比對、兩個 contract_test.go 與 hub_test.go、integration.test.mjs、m7purge.go、p2.md。使用者終端已見 `TestA99RT004` 同版通過；runtime 測試結果引用 p2.md 記錄，其準確性由驗證流程負責。

結論：無 blocking 缺陷；mapping 完整、無虛構行為、production/spec 依歸屬未被 P2 觸及。
