# 架構決策記錄

2026-11-21 建立的開發基線；依使用者「開始 M0」授權作可逆工程選擇，並非團隊人員已簽核或 Runtime 已通過驗收。產品語意仍以實作規範 v1.3 為準。

| 決策 | 內容 | 責任角色 |
|---|---|---|
| [ADR01](ADR01-package-boundaries.md) | Package、Port 與依賴規則 | 技術負責人／Go A |
| [ADR02](ADR02-module-lifecycle.md) | 模組版本語法、生命週期 fixture | Go A |
| [ADR03](ADR03-uow-profile.md) | UOW 契約及 timeout profile | Go A／QA |
| [ADR04](ADR04-database-roles.md) | DB 角色、RLS 與 Scope admission | Go C／Go A |
| [ADR05](ADR05-broker.md) | NATS JetStream 與可靠確認 | Go B／SRE |
| [ADR06](ADR06-errors-idempotency.md) | 錯誤、冪等與保留配置 | Go A／Go C |
| [ADR07](ADR07-migrations.md) | Goose、執行鎖與 schema ownership | Go A／SRE |
| [ADR08](ADR08-distribution.md) | module path、版本與發行平台 | 技術負責人／Go C |
| [ADR09](ADR09-scope-profiles.md) | 組織模組與替換 fixture | Go C／QA |
| [ADR10](ADR10-source-ownership.md) | M1 後續：程式碼歸屬、範例集中與搬移相容性 | 技術負責人／Go A |
| [ADR11](ADR11-product-version.md) | M1–M6 的產品版本固定 0.1.0，與文件／工具／schema 版本分離 | 技術負責人／Go A |
| [ADR12](ADR12-documentation-site.md) | Docusaurus 官方文件、版本快照、本機搜尋與 Docker | 技術負責人／Go A |
| [ADR13](ADR13-m3-security-and-delivery.md) | M3 當前權威、治理、可靠配送與 Goose 完整操作鎖 | 技術負責人／Go A |

責任角色尚未指派實際人員。核心公開 Port 已凍結為可編譯宣告並以 API surface gate 保護；95 組案例已拆成 421 個具名子情境，fixture、Profile 適用性、排程假設及驗證入口已固定。同 commit 的 M0 gate 證據由 `scripts/m0.py` 收集。Runtime 功能依原正式交付節點實作，不以 M0 通過取代。

- [ADR13：首版 CLI 的同版 Runtime Bundle](ADR13-cli-runtime-bundle.md)
