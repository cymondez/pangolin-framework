# Features

`New(settingsResolver, authorizer, definitions)` 提供 Boolean、非負整數 Quota、Enum。Evaluate 先以 policy.Require 驗證可信 Scope 及 Permission，再讀 Settings，Feature 開啟不授予任何權限。各 feature 的設定範圍與繼承由 Settings Definition 決定。

Feature 受 Permission 限制。Permission 拒絕或授權來源故障時，Evaluate 直接回傳錯誤，不讀取 Settings。Permission 允許後，才判斷功能開關、配額或等級。Feature 的值不會反向修改 Permission 判定；普通 Permission 檢查保持自己的授權責任。

| Permission | Boolean Feature | Evaluate 結果 |
|---|---|---|
| 拒絕 | true 或 false | 授權錯誤，不讀取功能設定 |
| 允許 | false | Boolean=false，功能不可用 |
| 允許 | true | Boolean=true，仍須遵守 Scope 與資源政策 |

Definition.ScopeKinds 可明示允許的 Scope Kind；空值表示所有可信 Kind，非空清單以外拒絕。它只限制可用範圍，不建立 Scope 授權。

一般產品設定使用最優先值。明示 Ceiling 的安全／配額限制對所有候選（含 Default）取交集：Boolean AND、Quota 最小值、Enum 依 Definition.Options 明示的由低至高順序取最小等級。下層不能放寬父層上限；Default 必須配置為適當的產品最高上限。Enum 非任意集合繼承。

錯誤保留授權／來源 cause；非法型別、負配額、未知 enum 與重複 enum 選項回 FEATURE_INVALID。無背景資源需清理。`go test ./platform/features`；持久化來源由 Settings Provider 提供，Features 不實作管理 UI。

功能值解析與權限判斷維持各自獨立職責；本套件在 `Evaluate` 組合兩者，固定先驗證 Permission、後解析 Feature。
