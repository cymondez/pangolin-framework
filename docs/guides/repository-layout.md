# 倉庫目錄與程式歸屬

以擁有者決定位置，再於擁有者內分層。根目錄 go.mod 定義唯一 module，公開 framework package 直接位於其下；不增加 src。只有實際交付的目錄才建立，規範 §10 的完整目標結構不等於當前檔案清單。

```text
kernel/、persistence/、security/、events/   共用框架公開能力
modules/organization/contracts/            尚待完整模組實作的中立管理 Port
examples/bookstore/
  cmd/api/、cmd/worker/                    範例的兩個可執行入口
  bootstrap/                              模組、Provider 與程序組裝
  domain/、application/                    業務規則與 Use Case
  infrastructure/                         Repository、SQL／sqlc、事件與 Read Model 薄片
  transport/http/                         範例專用 HTTP Handler
  internal/testkit/cmd/                    範例專用故障／對帳程序
internal/testkit/                         共用測試 IdP／Profile fixture
internal/quality/                         非公開 Go 品質檢查實作
tests/                                   跨套件驗收、契約／fixture／coverage 資料
tools/                                   Go 品質命令與工具索引
scripts/                                 建置／驗證操作入口與 Python 支援程式
design/                                  正式實作規範、架構總覽、里程碑
docs/                                    ADR、操作指南、遷移及驗收紀錄
deploy/                                  固定工具鏈、Compose 與部署配置
artifacts/、.cache/                       執行產物／快取，Git 排除
```

## 新檔案放哪裡

| 問題 | 歸屬 |
|---|---|
| 能否不依賴 Bookstore 及任何範例，供其他應用使用？ | 共用 framework package；禁止 import examples |
| 是否知道 Book、Bookstore 路由或其 Read Model？ | examples/bookstore 的對應層 |
| 是否只用於 Bookstore 故障或結果對帳？ | examples/bookstore/internal/testkit |
| 是否是多個測試共用、不能成為產品 API 的 fixture？ | internal/testkit |
| 是否測同一套件的行為？ | 與實作並置的 `_test.go` |
| 是否跨 package／真實服務驗收，或保存規範計畫？ | tests |
| 是否建置／執行／驗證工具？ | scripts 或 tools，列入工具索引 |
| 是否是產生的 SQL wrapper？ | 留在擁有者自己的 internal/bookdb，從 SQL source 再生成 |

Bookstore 專用 main 及 Handler 已集中於範例。根目錄 cmd 將保留給尚未交付的框架 CLI，共用 transport 將保留給不認識範例模型的入口機制；目前不為它們建立空目錄或轉接層。

## 閱讀與操作入口

- [Bookstore](../../examples/bookstore/README.md)：業務分層、API／Worker 與設定。
- [測試索引](../../tests/README.md)：各層測試、fixture、規範資料與結果。
- [工具索引](../../tools/README.md)：完整 Gate、檢查命令與操作腳本。
- [搬移對照](../release-notes/source-layout.md)：既有命令與 import 的更新方式。
- [ADR10](../adr/ADR10-source-ownership.md)：決策、相容性與驗證要求。

每個模組 README 隨程式一起維護。歷史 evidence 保留當時的來源 commit／路徑；不能因本次搬移重寫舊 report、原始 profile 或 hash。
