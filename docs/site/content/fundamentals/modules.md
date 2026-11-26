---
title: 功能模組
description: 將應用功能拆成 Module，以 Type 參照宣告相依，由 Root 裝配聚合應用。
---

當你的應用開始包含帳號、商品或訂單等功能時，可以用 Module 將它們分開組織。Module 是一個普通 Go struct 型別，宣告身分、相依與（需要的話）啟停方法。

先完成[第一個應用](../getting-started/quickstart.mdx)，再把其中的功能放進自己的模組。

## 定義自己的模組

模組身分由 `ModuleMetadata()` 方法提供；啟動工作以 `Start` 方法宣告：

```go
type booksModule struct{}

func (m *booksModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "books", Version: "0.1.0"}
}

func (m *booksModule) Start(context.Context, module.Runtime) error {
	fmt.Println("Books module is ready")
	return nil
}
```

只有一個模組時，把它當成根模組交給 `app.Run(ctx, &booksModule{})`（[第一個應用](../getting-started/quickstart.mdx)）。不需要額外欄位時，也可以改嵌入 `module.Base{Name: "books", Version: "0.1.0"}` 取得身分，不用再寫 `ModuleMetadata`。

## 宣告另一個模組

例如 `books` 必須等 `storage` 啟動後才能工作，以 Type 參照宣告相依，並用 `Requires` 附上版本約束：

```go
func (m *booksModule) DependsOn() []module.Ref {
	return []module.Ref{module.Of[storageModule]().Requires("=0.1.0")}
}
```

之後只要把 `books` 當根模組交給 `app.Run`：loader 沿著 Type 參照閉合相依圖，`storageModule` 自動納入規劃，多個模組共用的相依只規劃一次，並在任何模組工作開始前拒絕重名、缺漏相依、循環與版本不符。缺少依賴的應用不會啟動，不會留下看似可用的狀態。

完整示範（含相依順序與反序停機）見[應用啟動與停止](application-startup.mdx)；多模組契約組合的正式示範在[依賴注入](dependency-injection.mdx)說明，也可以直接從套件來源執行 `examples/moduleapp`。

## 什麼工作放在哪裡？

| 階段 | 你應該做的事 |
|---|---|
| PreConfigure／Configure／PostConfigure | 註冊服務建構子（`b.Services().Add`）與模組預設設定（`b.SetDefault`） |
| Start | 使用已組裝的服務，啟動 listener 或背景工作 |
| Stop | 停止模組自行擁有的工作 |

Runtime 先完成**所有**模組的三個組態階段、封存服務組合，再依相依順序逐模組 Start；停機時反序 Stop。三個組態階段與 Start／Stop 都是可選方法。

服務建構子若回傳 Cleanup，資源由組合容器在應用收尾時關閉，通常不需要又在 Stop 關閉一次。接著閱讀[依賴注入](dependency-injection.mdx)。

## 進階相容路徑

舊式明示清單組裝仍然可用並持續支援（未棄用）：定義 `module.Module{...}` 結構值（以欄位填鉤子、以 `module.Dependency` 填相依），把切片交給 `Host.Start(ctx, modules)`。維護以這種形狀組裝的既有應用時使用，完整對照見[應用啟動與停止的進階相容路徑](application-startup.mdx)。
