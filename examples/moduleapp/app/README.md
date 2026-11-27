# moduleapp/app：應用根模組

`Module` 是本示範的 Root 模組：`ModuleMetadata` 宣告名稱／版本，`DependsOn()` 以 Type 參照直接依賴 catalog 與 pricing。根模組自身不註冊任何 Provider——一般模組只宣告 Type 相依，並在 Start 透過受限 `rt.Services()` 解析 catalog 所 Export 的 `contracts.Catalog` 契約。消費方不需要相依實作模組本身。

錯誤行為：契約無法解析或應用資料不可用時，Start 回傳錯誤，Runtime 反序回收已啟動模組並以非零退出。
