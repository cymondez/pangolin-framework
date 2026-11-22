# panGOlin 官方文件網站

Docusaurus 3.10.2、React 與 Markdown／MDX，使用 `@easyops-cn/docusaurus-search-local` 生成中文／英文靜態搜尋。產品版本仍是根目錄 [VERSION](../../VERSION) 的 0.1.0；網站引擎及第三方套件版本各自獨立。技術決策見 [ADR12](../adr/ADR12-documentation-site.md)。

**讀者是套件使用者。** 依 ABP 的開始使用／循序教學／功能指南／參考分類，從建立使用者自己的 Go 專案開始。公開內容不放規格、Gate、驗收證據、內部里程碑或討論紀錄。自架網站與維護流程放在本 README；套件使用範例放在 [examples/basics](../../examples/basics/README.md) 的獨立 module。

`sync-snippets.mjs` 在開發／建置／文件版本命令之前，從實際範例生成被忽略的 `content/_snippets/*.go`；MDX 使用相對路徑讀取。正式快照會連同片段一起保存，後續更新範例不改寫舊版文件。不要手動維護 current 的生成片段；versioned_docs 內已封存的片段則隨版本追蹤。原生版本回歸同時修改 current 的 Go 範例，檢查舊版內容及範例都保持不變。

新增教學時交代前置需求、工作目錄、完整程式、執行命令、預期輸出與下一步。以 `python scripts/docs_examples.py` 在獨立使用者專案執行範例，另依 [初學者檢視](../guides/docs-beginner-review.md) 完成實際閱讀／操作檢查。

## 目錄與維護責任

| 位置 | 責任 |
|---|---|
| `content/` | 使用者手冊；教學、現有 API、支援邊界與部署 |
| `src/pages/`、`src/css/`、`static/` | 網站首頁、版本頁、樣式與本機資產 |
| `src/theme/Footer/LinkItem/` | 頁尾文件連結沿用目前閱讀版本；Navbar 使用原生 doc item |
| `docusaurus.config.js`、`sidebars.js` | URL、版本標示、搜尋與導覽 |
| `scripts/` | 版本檢查、靜態連結檢查、原生快照驗證與 Docker 版本注入 |
| `Dockerfile`、`compose.yaml` | Node 建置 → 非 root Nginx 執行 |
| `versioned_docs/`、`versioned_sidebars/`、`versions.json` | 首次正式發布後才產生並追蹤的快照 |

新增交易傳播教學使用第七份程式 transactions；設定 disposable Bookstore 的 PANGOLIN_DSN 後執行 `python scripts/docs_examples.py --database`，實際驗證父回滾／子提交。

Go 模組 README 仍維護該 package 的責任／API／設定／測試；網站維護使用者流程。變更模組時同步修改對應教學，不將規劃中的 API 寫成已可執行。ADR／evidence 留在各自目錄，網站不直接複製或發布全部維護檔案。

## 網站配色

採用「晨光藍砂」：霧藍 `#9CB8D0`、深藍灰 `#2D3F50`、暖沙 `#D7AB82` 與奶油色 `#F7E0BE`。首頁使用柔和藍砂漸層，文件閱讀區以奶油白 `#FFFCF7` 為底；文字與主要按鈕使用深藍灰，暖沙色用於重點與選取標記。共用色彩集中在 `src/css/custom.css` 的 `--pg-*` tokens，並對應 Docusaurus 的 `--ifm-*` 變數；Logo、分享圖與 Prism 程式碼配色一同維護。

首次瀏覽預設為淺色，不隨作業系統切換；使用者仍可透過頁首手動選擇藍灰深色模式，網站會保留既有選擇。調整樣式後執行 production build，並檢視首頁、文件／範例頁、搜尋、手機導覽與明暗模式。

## 本機開發與檢查

Node.js 22.x；Windows 使用 npm.cmd，Linux 可用 npm。從本目錄執行：

```powershell
npm.cmd ci
npm.cmd start -- --port 18088
```

Production build 才會生成完整搜尋索引：

```powershell
npm.cmd test
npm.cmd run build
npm.cmd run serve -- --port 18088
```

`build` 同時檢查產品版本、Docusaurus 的斷鏈與 Markdown 參照、輸出 HTML 的本機路徑／錨點、靜態搜尋與版本 metadata。所有檔案生成於被忽略的 `build/`、`.docusaurus/`、`.cache/`，不提交依賴或產物。

原生版本快照／同頁切換／子路徑的完整回歸：

```powershell
node scripts/check-versioning.mjs
```

此測試在站點 `.cache/docs-versioning-*` 複製隔離 fixture，透過上層目錄使用現有 node_modules；在 fixture 保存或使用既有產品快照，再修改 current 驗證內容／搜尋互不污染，並建置 `/framework-test/` 子路徑。它不建立正式產品 tag 或 checkout 的發行快照，發布後也能繼續驗證。fixture 保留供檢查。

## Docker

從 repository root 先盤點 Docker／Compose 專案、port、volumes／network 與資源。確認名稱與 18088 未被其他工作使用後：

```powershell
node docs/site/scripts/docker.mjs up -d --build --wait --wait-timeout 60
```

開啟 `http://localhost:18088`。此命令不需要本機 npm install，Node 只執行標準函式庫腳本，依賴安裝與建置都在 Docker build 內完成。腳本從 VERSION 注入 image tag／build arg；底層 Compose 缺少 PANGOLIN_VERSION 會拒絕。

部署主機只有 Docker、沒有 Node 時，也可直接從 VERSION 注入環境變數：

```powershell
$env:PANGOLIN_VERSION = (Get-Content VERSION -Raw).Trim()
docker compose -p pangolin-docs -f docs/site/compose.yaml up -d --build --wait --wait-timeout 60
```

此方式仍在容器內完成 Node 建置，執行容器只有 Nginx。

預設 project 為 `pangolin-docs`，loopback port 18088、獨立 network、無持久化 volume。Nginx 以非 root、唯讀 filesystem、16 MiB `/tmp`、64 MiB／0.5 CPU／64 pids 執行。Node／Nginx 基底固定官方 index digest。資源只服務靜態內容與本機搜尋，無外部字型、搜尋帳號或 API key。

```powershell
node docs/site/scripts/docker.mjs ps
node docs/site/scripts/docker.mjs logs --tail 50
# 再次確認 project 歸屬後停止自己的服務：
node docs/site/scripts/docker.mjs down
```

`PANGOLIN_DOCS_PROJECT`／`PANGOLIN_DOCS_PORT` 可改自己的隔離名稱／port。`DOCS_URL` 與 `DOCS_BASE_URL` 是建置參數，修改後要重新 build。子路徑需 `/framework/` 形式；反向代理原樣轉送前綴，由 Nginx 讀取相同前綴的實體靜態檔案。正式 origin 未指定時預設 localhost，不代表已綁定公開網域。

## 版本與發布

M1–M6 更新同一份 0.1.0 文件。正式發布 Gate 通過後才執行 `npm run docusaurus -- docs:version 0.1.0`，追蹤產生的快照與版本清單。網站會將 current 移至 Next（開發中），以凍結的 0.1.0 作預設。未來更新 VERSION 時同步 package.json 與 lockfile，版本不符會失敗。

頁首原生版本選單切換到同一 doc ID 的另一版；該頁不存在時退回該版首頁。搜尋索引由 plugin 按版本建立，不混合不同版本的手冊。現階段只有實際的 0.1.0 開發版，不建立虛構的歷史版本。

參考：[Docusaurus 版本管理](https://docusaurus.io/docs/versioning)、[靜態部署](https://docusaurus.io/docs/deployment)、[搜尋插件](https://github.com/easyops-cn/docusaurus-search-local)。
