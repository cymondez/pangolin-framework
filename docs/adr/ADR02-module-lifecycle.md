# ADR02 — 模組版本與啟停

狀態：採用於 M0；API／實作待 M1。對應 ENV04、MOD-001–003、A01、A02、A46。

模組版本採 SemVer 2.0.0。首版依賴範圍限定完整三段數字的 `=x.y.z`，或以空白 AND 連接的 `>=x.y.z <x.y.z`；不接受隱含 wildcard、caret、tilde、OR 或缺版本。預發行版本只允許精確相等，build metadata 不影響 precedence。未支援語法在 Plan 時拒絕，不能寬鬆解析。

維持規範 §2 的 Module／Dependency／Registry，穩定拓撲排序採逐層 Kahn 排序、每層依 Name 排序。全部 PreConfigure → 全部 Configure → 全部 PostConfigure → 驗證／Seal → 依序 Start；非逐模組跑完全部階段。

fixture：重名、缺相依、版本不符、循環、同層排序；第三個 Start 取得部分資源後失敗，自己清理部分資源，前兩個反序 Stop；每項資源只關閉一次。不以空測試假裝通過 fixture。

Plan、Builder、Runtime／Provider 的簽章已凍結於 kernel/*/contracts.go 並可獨立編譯。Builder／Runtime 採介面值以避免 module/runtime 循環 import；依賴生命週期與 partial Factory cleanup 細則已納入實作規範 §2.2。fixture 向量位於 tests/fixtures/modules.json；不執行尚未交付的 Runtime。
