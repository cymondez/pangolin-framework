---
title: 功能模組
description: 將應用功能拆成 Module，宣告依賴並接入應用生命週期。
---

當你的應用開始包含帳號、商品或訂單等功能時，可以用 Module 將它們分開組織。Module 是普通 Go 值，明確宣告名稱、版本、相依與啟停工作。

先完成[第一個應用](../getting-started/quickstart.mdx)，再把其中的 `app` 改成自己的模組。

## 定義自己的模組

```go
app := module.Module{
    Name: "books",
    Version: "0.1.0",
    Start: func(ctx context.Context, app module.Runtime) error {
        fmt.Println("Books module is ready")
        return nil
    },
}
```

這段放在既有 `main.go` 的 `run` 函式內，使用原本的 `context`、`fmt` 與 `kernel/module` imports，最後照樣把 `app` 交給 `host.Start`。

## 宣告另一個模組

例如 `books` 必須等 `storage` 啟動後才能工作：

```go
DependsOn: []module.Dependency{
    {Name: "storage", VersionConstraint: "=0.1.0"},
},
```

也要把 storage 模組放進傳給 Host 的切片。缺少依賴、同名或循環會讓啟動失敗，不會留下看似可用的應用。完整可執行版本見[多模組啟停](application-startup.mdx)。

## 什麼工作放在哪裡？

| 階段 | 你應該做的事 |
|---|---|
| Configure | 註冊服務與模組預設設定 |
| Start | 使用已組裝的服務，啟動 listener 或背景工作 |
| Stop | 停止模組自行擁有的工作 |

Configure 之前／之後也提供 PreConfigure 與 PostConfigure；一般應用可先只使用 Configure 與 Start。所有模組完成設定與服務組裝後才開始啟動。

服務建立時若已提供 DI Cleanup，資源由容器關閉即可，避免又在 Stop 關閉一次。接著閱讀[依賴注入](dependency-injection.mdx)。
