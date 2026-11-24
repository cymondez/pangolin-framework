---
title: Features 與配額
description: 在 Permission 允許後，判斷產品功能、配額與 Enum 等級。
---

先執行[分層設定完整範例](settings.mdx)。使用者設定 book_limit=50，但 Tenant 上限為 10，因此有效配額仍為 10；editing 即使是 true，使用者缺少 books.write 仍會被拒絕。

Permission 拒絕或授權來源故障時，Evaluate 不讀取 Settings。Feature 的值不反向改寫普通 Permission 判定；功能關閉不會把已有 Permission 改成拒絕，但該功能仍不可使用。

## 定義功能

features.New 接受 Settings Resolver、Authorizer 與功能定義。每個 Definition 明示 Setting、Permission、Kind，可另設 ScopeKinds 限制允許的資源範圍。ScopeKinds 為空時接受任何已授權的 Scope Kind；設定範圍與繼承則由 Settings Definition 決定。

Evaluate 先檢查可信 Scope 與 Permission，再檢查適用範圍及設定值。Feature 開啟不建立 membership、不授予 action，也不取代 Resource Policy。

| 型別 | 值 | 非法值 |
|---|---|---|
| Boolean | true／false | null、數字、字串 |
| Quota | 非負整數 | 負值、小數、null |
| Enum | Definition.Options 中的字串 | 不在選項內的值 |

結果 Value 帶有版本，並提供對應的 Boolean、Quota、Enum 欄位。未知功能或非法型別回 FEATURE_INVALID；授權與來源錯誤保留 cause。

## 區分一般產品設定與上限

未啟用 Ceiling 時使用 Settings 最優先的值。安全／配額上限需明示 Ceiling=true，對所有候選值（包含 Default）收斂：

| 型別 | Ceiling 的合併方式 |
|---|---|
| Boolean | 全部為 true 才開啟 |
| Quota | 使用最小值 |
| Enum | 依 Options 明示由低至高的順序，使用最低等級 |

例如 Enum 的 Options=[basic, pro]，父層 basic 會限制下層 pro。這是有序產品等級，並非任意字串集合交集。Default 也參與上限計算，應設為允許的產品最高上限；不要以 Default=false 無意間關閉全部子層。

Features 沒有常駐資源，持久化由 Settings Provider 負責。下一步閱讀[授權](../security/authorization.mdx)與[資源 Scope](../security/resource-scopes.md)，確認自己的 membership、Permission、產品設定與 Resource Policy 各自負責什麼。
