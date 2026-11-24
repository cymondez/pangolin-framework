---
title: "panGOlin-framework｜里程碑規劃書"
subtitle: "交付順序與工作包｜相依、責任、累積驗證、需求映射與發布 Gate"
date: "2026-11-20"
lang: zh-TW
---

# panGOlin-framework｜里程碑規劃書

> **文件基線：** v1.3；承接整合稿 v1.2，產品目標 v0.1.0。<br>
> **狀態：** 已成文、待實作與證據驗收；文件檢查不代表功能已完成。

## 0. 計畫權威與目前狀態

本文件依[實作規範書 v1.3](panGOlin-framework_實作規範書.md)安排全框架 v0.1.0 的交付，包含可選的 Enterprise／Organization 單一模組。需求正式句、介面、產品行為、版本範圍、A01–A95 及品質門檻只由實作規範定義；本文件不重新定義它們。

**2026-11-24：M0 已 Verified（來源 commit `a0b1ad099fdf7bd28240514a24c34590220019a1`）；M1 已 Verified（來源 `9eb10df6cd848b87fda964f0b6b61d60dc12b0a6`），M2 已 Verified（來源 `23651f654389af99d8c427b11909c07bc70f58cc`）；M3 已 Verified（來源 `7f17ecc76a84e65ec4d64fb39b0900fb6bb50ba7`）；M4 已 Verified（來源 `43dcd79e24f5ed74a544a760301f4fec95dc2ca2`，見 [M4 驗收紀錄](../docs/evidence/M4.md)）；M5 已 Verified（後續修正來源 `0d811a21f8b599196f0bad2dd2ee9033d1aa184b`，見 [M5 修正驗收](../docs/evidence/M5-corrections.md)）；M6 已 Verified（來源 `1d7ef1cf0dfdb7ee7b0211d04ad3380329883cb1`，見 [M6 驗收紀錄](../docs/evidence/M6.md)）。** 已建立的底座及未完成條件見 §2.1。文件、CI 配置或品質工具測試不等於 M0 通過，也不等於 Runtime 已驗收或全框架達成 90% coverage。只有指定 commit 的 Exit 證據完整且 Gate 通過才能改為 Verified。

## 1. 範圍、估算與角色

### 1.1 發布集合與延期項目

v0.1.0 交付集合為實作規範 §5.7 與 §16 的全部 95 項需求；唯一逐項清單在本文件 §5。產品範圍以[實作規範 §15](panGOlin-framework_實作規範書.md#s-15)為準。v0.2+ 的 Redis、Blob、進階 Cron 及新增 Adapter 未分配本版工作量，也沒有虛構的已交付 ID；若納入，先修改規範、案例與估算。

Organization 模組對採用框架的專案可選，對 v0.1.0 框架發行包仍是交付項。純 Tenant、組織啟用與替換契約三種組裝依[實作規範 §13.4](panGOlin-framework_實作規範書.md#s-13.4)驗證。

### 1.2 工期基線與容量假設

原估算為 3 名全職 Go 工程師、QA／SRE 各 0.5 名，24 週、12 個兩週 Sprint。Organization／Scope 整合是新增工作量；下列週數保留原相依節奏，**不是新增範圍後仍可在 24 週完成的承諾**。W1 從正式啟動日起算，目前沒有日曆開工／發布日期。

M0 逐工作包重估工時、測試／遷移成本與關鍵路徑，形成可追溯的新版排程；若超出容量，調整日期或經範圍變更程序改版，不減少必跑案例或降低品質 Gate。每個 Sprint 結束依已驗證增量更新剩餘估算。

| 規劃角色 | 主要責任 | 容量／指派狀態 |
|---|---|---|
| 技術負責人 | 契約一致性、ADR、相依與 API 審查、凍結／變更判定 | 由 3 名 Go 工程師之一兼任；人選待專案指派 |
| Go A：Core／Persistence | Runtime、DI、UOW、Repository、API 邊界 | 規劃 1 FTE；非已指派人名 |
| Go B：Events／Operations | Outbox／Inbox／Broker、Jobs、Telemetry／Drain | 規劃 1 FTE |
| Go C：Security／Organization | Tenant／Scope、授權、Organization、平台／CLI | 規劃 1 FTE；負載在 M0 重新分配 |
| QA | 符合性矩陣、故障時序、測試有效性及證據審查 | 規劃 0.5 FTE；開發者仍負責自身測試 |
| SRE | CI 環境、Broker／DB、容量、停機及復原演練 | 規劃 0.5 FTE |

### 1.3 M0 工作量重估草案

以下為依工作包責任進行的 bottom-up 初估（人日），包含開發者自身的測試、文件及回歸；QA／SRE 欄為額外審查與演練成本。沒有實際 throughput／指派人員的校正資料，信心低，不是人力或交期承諾。

| 工作包 | Go 工程 | QA | SRE | 主要估算驅動 |
|---|---:|---:|---:|---|
| WP01 | 8 | 2 | 5 | 決策、環境與故障開關 |
| WP02 | 10 | 4 | 4 | collector、分母及 CI fixture |
| WP03 | 20 | 2 | 2 | DAG、DI、生命週期與失敗清理 |
| WP04 | 12 | 2 | 1 | 真實 HTTP／DB 垂直片 |
| WP05 | 18 | 3 | 3 | JWT／RLS／可靠事件薄片 |
| WP06 | 28 | 4 | 1 | UOW 選項與巢狀矩陣 |
| WP07 | 20 | 5 | 2 | Unknown、取消及時序故障 |
| WP08 | 18 | 3 | 1 | Scope Port 與兩 Org 薄片 |
| WP09 | 28 | 5 | 3 | 隔離、安全與 admission |
| WP10 | 30 | 5 | 1 | 組織治理、角色與撤權競態 |
| WP11 | 20 | 4 | 4 | 替換、policy 與前向遷移 |
| WP12 | 26 | 5 | 4 | Broker／Lease／Inbox 併發 |
| WP13 | 18 | 4 | 1 | 冪等併發及 Unknown 對帳 |
| WP14 | 24 | 4 | 7 | Jobs、容量及恢復 |
| WP15 | 12 | 4 | 10 | 觀測、負載與停機演練 |
| WP16 | 18 | 3 | 1 | Scoped Cache／Settings／Features |
| WP17 | 24 | 4 | 3 | CLI、生成物與兩 Profile |
| WP18 | 16 | 6 | 12 | 全量、安全、升級及復原證據 |
| **合計** | **350** | **69** | **65** | 不另將 AI 速度視為已量測產能 |

以原容量 3 Go FTE、QA／SRE 各 0.5 FTE、每週 5 工作日，單看容量下界分別為 23.3／27.6／26.0 週；另受相依與整合影響。新版規劃窗口暫估 32–36 週，待人力與 S2／S3 實測更新；原 W1–W24 保留為歷史節奏，不再作本版預測。

| 節點 | 新的相對週數草案 | 關鍵相依 |
|---|---|---|
| M0 | W1–2 | 公開 Port／環境／fixture／估算 Gate |
| M1 | W3–7 | M0，接通真實薄流程 |
| M2 | W8–13 | M1，完整 UOW 與 Scope 核心 |
| M3 | W14–22 | M2，安全／組織／事件／遷移整合 |
| M4 | W23–27 | M3，Jobs 與負載／恢復 |
| M5 | W28–32 | M4，平台與兩種生成物 |
| M6 | W33–36 | 全量回歸及發布證據 |

Go C 的組織／Scope／CLI 負載由 Go A 協作 owner schema／migration、Go B 協作 Scope envelope／Job 傳播與 CLI 事件範本；仍以公開 Port 先固定後分工。M0 的容量模型沿用 §1.2 的 3 Go FTE、QA／SRE 各 0.5 FTE，並以上列逐包人日重估及負載重分配作為可追溯基線。責任按角色指定，不虛構人員任命；實際人員與日曆日期未定只限制交期承諾，不額外增加原 M0 Exit 的人事核准條件。

<a id="decisions"></a>

## 2. M0 決策與開工條件

ENV01–ENV06 的待選值由[實作規範 §11.2](panGOlin-framework_實作規範書.md#s-11.2)列出。以下是決策責任與完成物，不重開已確立的 UOW／Scope 行為。

| ADR | 決策／產物 | 負責角色 | 對應環境決策 | 完成 Gate |
|---|---|---|---|---|
| ADR01 | Package／公開 Port、依賴檢查規則 | 技術負責人、Go A | ENV04 | M0 Exit |
| ADR02 | Module 版本範圍語法、DAG／Lifecycle fixture | Go A | ENV04 | M0 Exit |
| ADR03 | UOW 契約採納、API 命名、timeout／depth profile | Go A、QA | ENV05 | M0 Exit |
| ADR04 | RLS／Host／Worker 系統表角色、Profile 遷移與 Scope admission | Go C、Go A | ENV01 | M0 Exit |
| ADR05 | 單一 Broker、拓樸、可靠確認、Envelope fixture | Go B、SRE | ENV02 | M0 Exit |
| ADR06 | Error／Idempotency 組裝及保留／重試配置 | Go A、Go C | ENV06 | M0 Exit |
| ADR07 | 單一 Migration Tool、執行鎖、schema ownership | Go A、SRE | ENV03 | M0 Exit |
| ADR08 | module path、CLI／Template／發布版本綁定 | 技術負責人、Go C | ENV01、ENV04 | M0 Exit |
| ADR09 | 中立 Scope／單一 organization 模組、替換 fixture、catalog 相容性 | Go C、QA | ENV05、ENV06 | M0 Exit |

共同完成物：固定版本／image digest 的環境清單、必填配置 Profile、兩種產品 Profile 與替換測試 fixture 的設計、故障注入入口、95 項驗收骨架及分母 manifest 規劃、包含新增模組的工期重估。每項 ADR 記錄責任角色、選擇、理由、證據及相容性影響；未定案阻擋 M0，不以「待實作自行選擇」結案。

### 2.1 M0 執行狀態（2026-11-21）

工作分支 `codex/m0-foundation`，module path 依使用者新增的 origin 固定為 `github.com/cymondez/pangolin-framework`。ADR 見 `docs/adr/README.md`；環境與配置見 `deploy/`；驗收底稿見 `tests/contract/`。

| 項目 | 狀態／證據 | 尚未滿足的 Exit 條件 |
|---|---|---|
| ADR01–09／ENV01–06 | Go module／版本／Broker／Migration Tool／local profile 固定；核心 contracts.go 與 API surface 可編譯並有漂移檢查 | 已由同 commit Gate 驗證 |
| Go／CI runner | Windows 本機與固定 digest 的 Linux OCI runner；同一 scripts/m0.py verify 入口供 GitHub Actions 使用 | 本機 runner 與同 commit 證據作 M0 驗收；遠端 GitHub Actions 執行另列、不虛報 |
| 95 項驗收／Profile | 421 個具名子情境及三種適用性矩陣；錯誤 N/A／缺漏／假 verified 會被拒絕 | 正式 Runtime 全部 planned，依 M1–M6 原映射交付 |
| Fixture／故障裝置 | 四份固定 seed 的 fixture、三個組裝 Port、獨立 PG／JetStream／IdP／Toxiproxy；使用新 namespace／空 volume 驗證 | 新 namespace／空 volumes 的同 commit 9 項 smoke 已通過 |
| Coverage／證據 | 原始計數 fixture、API／import／配置／Profile／same-commit 防護；每個 runner 保存 log／metadata／hash／raw profile | E2E instrumentation／跨 suite 合併仍是 M1 工作，不冒稱 M0 已有 Runtime E2E |
| 估算／容量 | §1.3 的逐工作包估算、角色負載調整及相對週數已固定為計畫基線 | 隨實際交付速率更新，不承諾未確認的人力或日曆日期 |

**M0 Verified**：Windows、固定 OCI Linux runner（含 race）及空環境 report 均綁定上述乾淨來源 commit，M0 gate 通過。完整結果與 hash 見 [M0 驗收紀錄](../docs/evidence/M0.md)。M0 Exit 驗證公開契約、可重現環境、驗收骨架與重估；它不要求將 M1–M6 的產品 Runtime 提前完成，也不將原計畫以外的人事／發布核准增設為門檻。

<a id="milestones"></a>

## 3. M0–M6 交付節點

```mermaid
flowchart LR
    M0["M0 契約／環境／估算"] --> M1["M1 真實垂直流程"]
    M1 --> M2["M2 完整 UOW／Scope 核心"]
    M2 --> M3["M3 安全／組織／可靠性"]
    M3 --> M4["M4 工作／運維韌性"]
    M4 --> M5["M5 平台／兩種 Profile CLI"]
    M5 --> M6["M6 全量驗收／發布"]
```

圖中為驗收相依，不禁止 Port 已固定後預先進行獨立工作。未通過前置 Gate 時，不把相依交付標為完成。正式 Delivers 只記一次；提前切片證據不等於整個子系統完成。以下 ID 清單是 §5 的閱讀索引，任何差異由一致性檢查阻擋。

### M0 — 契約、環境與估算底座

**狀態：Verified；證據來源 `a0b1ad099fdf7bd28240514a24c34590220019a1`。**

**原節奏：** W1–W2；**責任：** 技術負責人，Go A／B／C、QA、SRE 共同參與。  
**Delivers：** 無產品需求正式驗收；交付 §2 決策、環境與證據底座。

**Tasks**

1. 完成 ADR01–09 與 ENV01–ENV06，凍結公開契約及必要配置。
2. 建立最小 Go workspace、CI runner、Compose DB／Broker／測試 IdP 及連線故障代理。
3. 建立 A01–A95 skeleton／Profile manifest 與 coverage 計數 fixture；未實作的 runtime 案例保持待辦，不能回傳假成功。
4. 逐項估算 Organization／Scope 的新增成本、重新配置工作負載並更新里程碑日期。

**Exit：** ADR 有結論與理由；乾淨環境能建庫及 Broker 收發；故障開關可造成預期失敗；work package／驗收對應及重估可追溯。  
**Gate：** 契約、環境或估算缺一不進 M1 正式交付。

### M1 — 最小真實垂直流程與工程 Gate

後續目錄整理增量已以來源 `b3bf59c03f4b66ff7389d44d8434356258a85d9a` 通過相同 Windows／Linux（含 race）累積 Gate；未新增正式 Delivers 或改寫首次交付來源。見 [目錄整理回歸紀錄](../docs/evidence/source-layout.md)。

**狀態：Verified；來源 `9eb10df6cd848b87fda964f0b6b61d60dc12b0a6`，分支 `codex/m1-vertical-flow`。** WP02–WP05 的正式案例、S2／S3、Windows／固定 OCI Linux（含 race）同 commit Gate 全部通過；9 項正式需求、47 個具名子情境與 20 份模組說明已驗證。完整結果見 [M1 驗收紀錄](../docs/evidence/M1.md)，重現方式見 [M1 開發指南](../docs/guides/m1-development.md)。

**原節奏：** W3–W6；**責任：** Go A 主責，Go B／C 補事件及身分薄切片，QA／SRE 驗證。  
**Delivers：** `ARC-001`、`MOD-001`、`MOD-002`、`MOD-003`、`CFG-001`、`OPS-001`、`REL-001`、`REL-002`、`REL-004`。

**Tasks**

1. 完成 Kernel／DI／Config 的註冊、啟動、失敗清理及正常 Drain。
2. W4 接通 HTTP→CreateBook→Required Tx→PostgreSQL，驗證提交與回滾。
3. W6 加入真實簽章驗證、Tenant RLS、Outbox→真實 Broker→Inbox→Read Model，producer／consumer 分程序。
4. 從第一份 production code 啟用規範的 coverage／race／靜態分析／依賴 Gate 與同 commit 證據保存。

**Exit：** §5 的本節正式案例全部通過；S2／S3 的薄流程、回滾、重送與跨租戶拒絕可重現；CI 能阻擋故意植入的回歸。  
**Gate：** 薄路徑持續可運行，才擴充 M2。尚未正式交付的 UOW／事件完整矩陣不能宣稱完成。

### M2 — UOW／Persistence 與中立 Scope 完整

**狀態：Verified；來源 `23651f654389af99d8c427b11909c07bc70f58cc`，分支 `codex/m2-unit-of-work`。** WP06–WP08、36 項正式需求與 133 個具名子情境及 M1 累積回歸已通過同版 Windows／Linux（含 race）Gate。見 [M2 驗收紀錄](../docs/evidence/M2.md) 與 [M2 開發指南](../docs/guides/m2-development.md)。

**原節奏：** W7–W10；**責任：** Go A 主責，Go C 交付 Scope／模組薄切片，QA 注入故障。  
**Delivers：** `UOW-001`、`UOW-002`、`UOW-003`、`UOW-005`、`UOW-006`、`UOW-007`、`UOW-008`、`UOW-009`、`UOW-010`、`UOW-011`、`UOW-012`、`UOW-013`、`UOW-014`、`UOW-015`、`UOW-016`、`UOW-017`、`UOW-019`、`UOW-020`、`UOW-021`、`UOW-022`、`UOW-023`、`UOW-024`、`UOW-025`、`UOW-026`、`UOW-027`、`UOW-028`、`UOW-029`、`UOW-030`、`UOW-031`、`API-001`、`API-003`、`DATA-001`、`SCP-001`、`SCP-002`、`SCP-004`、`ORG-001`。

**Tasks**

1. 完成 UOW 選項、巢狀傳播、隔離、deadline、Panic／取消／清理／回呼與錯誤分類。
2. 完成 Repository CRUD／版本競爭、HTTP／串流短 Scope 邊界，加入連線池與鎖等待測試。
3. 完成中立 Scope Port、Kind 唯一性、組裝檢查及單一 organization 模組邊界。
4. 在 S4 驗證同 user 的兩 Org 資源交叉讀寫拒絕，同時保留 tenant-only 範例；此為完整模組之前的整合證據。

**Exit：** 本節正式案例與既有回歸全過；故障時點、Outcome、transaction ID 與 DB 狀態吻合；Scope 切換與已結束 context 重用被拒絕。  
**Gate：** M1 持續綠燈；涉及完整安全／事件原子性的剩餘 UOW 項目由 M3 正式驗收，不以章名推定已交付。

### M3 — 安全、Organization 與可靠事件完整

**狀態：Verified；分支 `codex/m3-security-reliable-events`。** 40 項正式需求、185 個具名情境與累積回歸在來源 `7f17ecc76a84e65ec4d64fb39b0900fb6bb50ba7` 通過同版 Windows／Linux（含 race）Gate，證據見 [M3 驗收紀錄](../docs/evidence/M3.md)；重現入口見 [開發指南](../docs/guides/m3-development.md)。

**原節奏：** W11–W14；**責任：** Go C 主責安全／組織，Go B 主責事件，Go A 主責資料與冪等；QA／SRE 共同驗收。  
**Delivers：** `UOW-004`、`UOW-018`、`UOW-032`、`API-002`、`EVT-001`、`EVT-002`、`EVT-003`、`EVT-004`、`EVT-005`、`EVT-006`、`EVT-007`、`EVT-008`、`EVT-009`、`TEN-001`、`TEN-002`、`TEN-003`、`SEC-001`、`SEC-002`、`SEC-003`、`DATA-002`、`IDEM-001`、`IDEM-002`、`IDEM-003`、`AUD-001`、`SCP-003`、`SCP-005`、`SCP-006`、`SCP-007`、`SCP-009`、`ORG-002`、`ORG-003`、`ORG-004`、`ORG-005`、`ORG-006`、`ORG-007`、`ORG-008`、`ORG-009`、`ORG-010`、`ORG-011`、`ORG-012`。

**Tasks**

1. 完整 JWT／Tenant／Permission／Host／Scope admission／RLS／Audit 與 owner 複合約束。
2. 完成 Enterprise／Organization／Membership／治理、撤權／轉移／停用／復原，以及替代 Provider fixture。
3. 完成 Outbox 租約、Broker 持久化與路由、Inbox 併發及外部副作用邊界。
4. 完成 Scope Envelope／Job 傳播契約、冪等併發與提交結果對帳；scope grant 與 Job 執行先用契約 harness 驗證，完整 Job Worker 在 M4。
5. 完成 tenant-only→組織 schema 前向遷移、替換遷移、非法卸載阻擋及安全負測試。

**Exit：** 本節正式案例與既有案例全部通過；兩 Profile 及替換 fixture 的適用安全契約通過；Commit Unknown、雙 Worker、崩潰重送與撤權競態有真實 DB／Broker 證據。  
**Gate：** 不用 Mock 宣稱資料庫或 Broker 語意成立；未完成的可靠性／資料隔離項目阻擋 M4 正式交付。

### M4 — 背景工作與運維韌性

**原節奏：** W15–W18；**責任：** Go B、SRE 主責，QA 與 Go C 協作。  
**Delivers：** `OPS-002`、`JOB-001`、`OBS-001`。

**Tasks**

1. 完成 Job registry、持久化狀態、租約／重試／取消／dead 及手動重送，整合 M3 Scope 契約。
2. 完成容量、背壓、告警、Trace／Metrics／Health 與敏感資訊遮罩。
3. 演練長期 Broker 故障、積壓恢復、活躍 HTTP／Worker 停機及 DB 最後關閉。

**Exit：** 本節正式案例及累積回歸全過；保留 lag／pool／drain／retry 曲線與恢復對帳，沒有靜默丟棄或跨 scope 工作。  
**Gate：** M3 契約閉環保持成立；M1 已交付的停機契約在此擴大負載驗證，不重複登記交付。

**2026-11-23：Verified。** 來源 `43dcd79e24f5ed74a544a760301f4fec95dc2ca2` 通過同版 Windows／Linux（含 race）累積 Gate，18 個 M4 情境及全部已交付回歸通過；64 producer／10,000 Job 全數排空，lag／pool／retry／drain 曲線與對帳見 [M4 驗收紀錄](../docs/evidence/M4.md)。

### M5 — 平台服務與開發者體驗

**狀態：** Verified；後續修正來源 `0d811a21f8b599196f0bad2dd2ee9033d1aa184b`，原 25 個正式情境、56 項必要單元回歸與 7 項 Backend 整合映射及同版 Windows／Linux（含完整 race）累積 Gate 通過，見 [修正驗收](../docs/evidence/M5-corrections.md)。原始交付 88f32eb6 的[驗收紀錄](../docs/evidence/M5.md)保留。

**原節奏：** W19–W22；**責任：** Go C 主責，Go A／B 補平台整合，QA 驗證生成物。  
**Delivers：** `PLAT-001`、`CLI-001`、`SCP-008`、`ORG-013`。

**Tasks**

1. 完成 Memory Cache／Settings／Features、Scope key／繼承／失效及 Audit 整合回歸。
2. 完成 CLI 全部 v0.1.0 命令、兩種 Profile Template、OpenAPI 與 Quickstart。
3. 在乾淨 runner 生成、build／test／啟動兩種 Bookstore；重跑安全與 UOW 契約。

**Exit：** 本節正式案例全過；新專案預設 tenant-only，明示選項啟用完整 organization 模組；命令重入不毀損已有檔案。  
**Gate：** 生成物依相同 Runtime／規範版本通過，不能靠文件步驟修補不完整範本。

### M6 — 全量驗收與發布證據

**2026-11-24：Verified。** 來源 `1d7ef1cf0dfdb7ee7b0211d04ad3380329883cb1`，分支 `codex/m6-release-acceptance`；WP18 的 13 個新情境及 M0–M5 累積回歸、95 項需求／421 個具名情境、三組 Profile、完整品質與發布演練通過同版 Windows／Linux（Linux 含完整 race）Gate。證據見 [M6 驗收紀錄](../docs/evidence/M6.md)，重現與清理見 [M6 開發指南](../docs/guides/m6-development.md)。歷史首次交付來源保留；沒有推論已建立 tag、推送或部署。

**原節奏：** W23–W24；**責任：** 技術負責人、QA、SRE；各工程師補足所負責的證據。  
**Delivers：** `REL-003`、`REL-005`、`ORG-014`。

**Tasks**

1. 執行 A01–A95 全部適用案例、三組 Profile 矩陣、完整品質 Gate 及安全審查。
2. 完成前向升級、備份復原、乾淨重建、負載基線與 runbook 實演。
3. 對齊 Runtime／CLI／Template／Event schema 版本，產出 release manifest 與變更說明。

**Exit：** §8 發布證據完整，全部需求正式映射為 Verified，沒有未知或多重交付 ID、未處理高風險缺陷或缺少適用案例。  
**Gate：** 未通過則延期；不把未完成項目自動移到下一版，也不把文件審查冒充執行驗收。

<a id="increments"></a>

## 4. 每兩週的漸進式驗證

| Sprint | 當期增量與真實相依 | 必須證明的場景 | 保留證據 |
|---|---|---|---|
| S1 W1–2 | 固定 Go／PG／Broker／Migration 版本，CI 環境及故障開關 | 空環境建庫、Broker 可收發、故障裝置有效；兩 Profile／替換契約 fixture 與新增工作估算完成 | image／tool 版本、seed、操作腳本 |
| S2 W3–4 | HTTP→CreateBook→UOW→PostgreSQL | 正常提交、第二次寫入失敗回滾、Commit 前不送 2xx；首次 schema 就有 tenant_id／RLS | API 回應、SQL 狀態、coverage、測試 log |
| S3 W5–6 | HTTP＋本機測試 IdP 真實簽章 JWT→Tenant→Tx→Outbox→真實 Broker→Inbox→Read Model | producer／consumer 分程序；回滾無事件；Broker 發布後 crash 可重送；基本跨租戶拒絕 | 端到端 trace、Event ID、DB 快照、重啟 log |
| S4 W7–8 | Required／RequiresNew、讀寫／隔離選項、SaveChanges；Scope Port／組織模組薄切片 | 捕捉內層錯誤仍 rollback-only；獨立子提交不被父回滾；模式衝突與 Scope 重用拒絕；同 user 在兩 Org 建立／讀取書籍，交叉存取拒絕，純 Tenant 範例仍可跑 | UOW 選項矩陣、transaction ID、DB 對帳 |
| S5 W9–10 | timeout／Panic／取消／回呼、完整 CRUD | Pool 耗盡、鎖等待、清理失敗、Commit Unknown、回應遺失、optimistic conflict | 故障時點、連線池前後值、Outcome 與錯誤鏈 |
| S6 W11–12 | 完整 JWT／Policy／Tenant／Organization／Enterprise／Audit 與 Scope 冪等 | 同鍵併發、DTO Tenant 偽造、跨租戶外鍵、回滾後安全日誌、JWKS 輪替；多組織角色／Enterprise 管理不等於內容授權、撤權／轉移 | 權限矩陣、攻擊案例與安全日誌 |
| S7 W13–14 | 完整 Outbox／Inbox／Broker 契約及遷移 | 雙 Worker Inbox、舊 Lease、無法路由、完整信封重啟復原、提交後崩潰冪等對帳；Scope envelope 重送、Provider 替換及非法停用拒絕 | Broker 設定、併發 barrier、消息／DB 對帳 |
| S8 W15–16 | Job 與持續故障 | retry／dead／手動重送、租戶竄改、長期 Broker 離線、容量上限 | retry 時間線、告警、dead 稽核 |
| S9 W17–18 | 運維與容量 | HTTP／Worker 有工作時停機、依賴最後關閉、復原排空、租戶負載競爭 | Drain 時間、Pool／Lag 曲線、runbook 實演 |
| S10 W19–20 | Memory Cache／Settings／Features | 跨租戶 key、不失權限的 Feature、失效失敗與版本／TTL 收斂 | 設定版本、快取隔離測試 |
| S11 W21–22 | CLI／Template／OpenAPI | clean runner 生成、測試及啟動兩種 Profile Bookstore，命令重入不毀損檔案 | 生成物 hash、build／test log、OpenAPI 報告 |
| S12 W23–24 | 發布候選 | 全量契約、90% gates、安全、前向升級、備份復原、效能基線 | 同一 commit 的發布證據包 |

每一節點重跑已交付案例及先前端到端流程；新增案例先以可重現失敗證明測試有效，再由實作使其通過。需要資料庫／Broker 語意的案例使用真實服務，Mock 僅用於純 Port 單元測試。測試中的 IdP 可為本機 fixture，但 JWT 簽章／Issuer／Audience／JWKS 驗證仍走正式實作。

每一切片至少保存：commit／規範基線、Profile、依賴版本、seed、命令及退出碼、預期／實際結果、適用的 DB／Broker／trace 證據，以及 [§13 品質 Gate](panGOlin-framework_實作規範書.md#s-13) 報告。只完成實作但沒有可重現證據，狀態仍是 In progress。

<a id="delivery-map"></a>

## 5. 唯一需求交付映射

本表是正式交付責任的唯一來源；每個需求恰有一個 delivering milestone。A 編號連至實作規範 §13.2 的 Setup／刺激／預期；各列證據依該章共同規則保存。後續整合或強化測試不更改正式交付次數。

| 里程碑 | 需求 ID | 驗收案例／Exit 證據 | 狀態 |
|---|---|---|---|
| M2 | UOW-001 | A17；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-002 | A18；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-003 | A19；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M3 | UOW-004 | A09；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M2 | UOW-005 | A20；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-006 | A03；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-007 | A21；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-008 | A22；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-009 | A23；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-010 | A24；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-011 | A25；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-012 | A26；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-013 | A27；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-014 | A28；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-015 | A29；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-016 | A30；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-017 | A31；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M3 | UOW-018 | A04；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M2 | UOW-019 | A32；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-020 | A05；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-021 | A33；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-022 | A34；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-023 | A35；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-024 | A36；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-025 | A37；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-026 | A38；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-027 | A39；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-028 | A40；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-029 | A41；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-030 | A42；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | UOW-031 | A43；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M3 | UOW-032 | A44；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M1 | ARC-001 | A45；同 commit 的執行結果與規範要求證據 | Verified（來源 9eb10df6；見 M1 驗收紀錄） |
| M1 | MOD-001 | A01；同 commit 的執行結果與規範要求證據 | Verified（來源 9eb10df6；見 M1 驗收紀錄） |
| M1 | MOD-002 | A02；同 commit 的執行結果與規範要求證據 | Verified（來源 9eb10df6；見 M1 驗收紀錄） |
| M1 | MOD-003 | A46；同 commit 的執行結果與規範要求證據 | Verified（來源 9eb10df6；見 M1 驗收紀錄） |
| M1 | CFG-001 | A47；同 commit 的執行結果與規範要求證據 | Verified（來源 9eb10df6；見 M1 驗收紀錄） |
| M2 | API-001 | A14；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M3 | API-002 | A48；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M2 | API-003 | A49；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M3 | EVT-001 | A50；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | EVT-002 | A51；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | EVT-003 | A11；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | EVT-004 | A52；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | EVT-005 | A53；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | EVT-006 | A06；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | EVT-007 | A54；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | EVT-008 | A55；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | EVT-009 | A16；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | TEN-001 | A08；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | TEN-002 | A56；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | TEN-003 | A57；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | SEC-001 | A12；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | SEC-002 | A13；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | SEC-003 | A58；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M2 | DATA-001 | A10；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M3 | DATA-002 | A59；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | IDEM-001 | A07；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | IDEM-002 | A60；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | IDEM-003 | A61；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | AUD-001 | A62；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M1 | OPS-001 | A15；同 commit 的執行結果與規範要求證據 | Verified（來源 9eb10df6；見 M1 驗收紀錄） |
| M4 | OPS-002 | A63；同 commit 的執行結果與規範要求證據 | Verified（來源 43dcd79e；見 M4 驗收紀錄） |
| M4 | JOB-001 | A64；同 commit 的執行結果與規範要求證據 | Verified（來源 43dcd79e；見 M4 驗收紀錄） |
| M5 | PLAT-001 | A65；同 commit 的執行結果與規範要求證據 | Verified |
| M4 | OBS-001 | A66；同 commit 的執行結果與規範要求證據 | Verified（來源 43dcd79e；見 M4 驗收紀錄） |
| M5 | CLI-001 | A67；同 commit 的執行結果與規範要求證據 | Verified |
| M1 | REL-001 | A68；同 commit 的執行結果與規範要求證據 | Verified（來源 9eb10df6；見 M1 驗收紀錄） |
| M1 | REL-002 | A69；同 commit 的執行結果與規範要求證據 | Verified（來源 9eb10df6；見 M1 驗收紀錄） |
| M6 | REL-003 | A70；同 commit 的執行結果與規範要求證據 | Verified（來源 1d7ef1cf；見 M6 驗收紀錄） |
| M1 | REL-004 | A71；同 commit 的執行結果與規範要求證據 | Verified（來源 9eb10df6；見 M1 驗收紀錄） |
| M6 | REL-005 | A72；同 commit 的執行結果與規範要求證據 | Verified（來源 1d7ef1cf；見 M6 驗收紀錄） |
| M2 | SCP-001 | A73；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M2 | SCP-002 | A74；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M3 | SCP-003 | A75；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M2 | SCP-004 | A76；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M3 | SCP-005 | A77；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | SCP-006 | A78；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | SCP-007 | A79；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M5 | SCP-008 | A80；同 commit 的執行結果與規範要求證據 | Verified |
| M3 | SCP-009 | A81；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M2 | ORG-001 | A82；同 commit 的執行結果與規範要求證據 | Verified（來源 23651f65；見 M2 驗收紀錄） |
| M3 | ORG-002 | A83；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | ORG-003 | A84；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | ORG-004 | A85；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | ORG-005 | A86；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | ORG-006 | A87；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | ORG-007 | A88；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | ORG-008 | A89；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | ORG-009 | A90；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | ORG-010 | A91；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | ORG-011 | A92；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M3 | ORG-012 | A93；同 commit 的執行結果與規範要求證據 | Verified（來源 7f17ecc7；見 M3 驗收紀錄） |
| M5 | ORG-013 | A94；同 commit 的執行結果與規範要求證據 | Verified |
| M6 | ORG-014 | A95；同 commit 的執行結果與規範要求證據 | Verified（來源 1d7ef1cf；見 M6 驗收紀錄） |

<a id="work-items"></a>

## 6. 工作包與執行規則

### 6.1 可拆成 Issue 的工作包

工作包不是新增需求 ID。建立 Issue 時引用本表相關案例，再由 §5 取得需求 ID 與正式責任；不得把整個里程碑當作單一不可檢查的大工作項目。

| 工作包 | 節點／主要角色 | 產物 | 完成依據 |
|---|---|---|---|
| WP01 契約與環境 | M0／技術負責人、SRE | ADR、配置、Compose、版本清單、重估 | §2 與 M0 Exit |
| WP02 測試與 CI 底座 | M0→M1／QA、Go A | case manifest、故障 harness、coverage collector | A68、A69、A71 |
| WP03 Runtime／模組 | M1／Go A | DAG、Registry、Config、Lifecycle、Drain | A01、A02、A15、A45–A47 |
| WP04 Bookstore DB 薄片 | M1／Go A | API／Domain／Repo／Required Tx | S2，作 M2 正式案例前置證據 |
| WP05 可靠事件薄片 | M1／Go B、Go C | 真 JWT／RLS、Outbox／Broker／Inbox／Read Model | S3，作 M3 正式案例前置證據 |
| WP06 UOW 狀態與選項 | M2／Go A | Options／Required／RequiresNew／SaveChanges | §5 中 M2 的 UOW 案例 |
| WP07 UOW 故障與 HTTP | M2／Go A、QA | deadline／Unknown／Panic／Cleanup／Callbacks | S5 與 M2 對應案例 |
| WP08 Scope 與模組薄片 | M2／Go C | 中立 Port、Kind 檢核、兩 Org Bookstore | A73、A74、A76、A82 與 S4 |
| WP09 身分與資料隔離 | M3／Go C、Go A | JWT／Policy／RLS／FK／Audit／Scope admission | M3 的 Tenant／Security／Scope 案例 |
| WP10 組織治理 | M3／Go C | Membership／Enterprise／轉移／撤權／復原 | A83–A90 |
| WP11 組織替換與遷移 | M3／Go C、Go A | Catalog、匯入／對帳、前向 migration、停用檢核 | A91–A93 |
| WP12 可靠配送完整 | M3／Go B | Lease、Broker confirm／routing、Inbox 鎖／重送 | M3 的事件案例及 S7 |
| WP13 冪等與對帳 | M3／Go A | 同鍵併發、成功結果、Unknown 復原 | A07、A60、A61、A81；回歸 A33 |
| WP14 Jobs／容量 | M4／Go B、SRE | Worker、dead／retry、背壓、恢復 runbook | A63、A64；回歸 A79 |
| WP15 觀測與停機演練 | M4／SRE、Go B | Trace／Metrics／Health、Drain／恢復證據 | A66；回歸 A15 |
| WP16 平台服務 | M5／Go C | Scoped Cache／Settings／Features | A65、A80 |
| WP17 CLI 與指南 | M5／Go C、Go A | 兩 Profile 範本、命令、OpenAPI、指南 | A67、A94 |
| WP18 發布驗收 | M6／技術負責人、QA、SRE | release manifest、全量報告、升級／復原演練 | A70、A72、A95 及全部累積 Gate |

### 6.2 Issue 與驗收證據格式

每個 Issue 記錄：工作包、正式交付節點、需求／案例及 subtests、負責角色、前置依賴、允許修改的 Port／schema、實作任務、可重現命令、預期結果及 evidence 路徑。開始前讀相關契約；變更後跑對應案例與受影響回歸；有實作差異先回報規範變更，不以私有捷徑繞過 Gate。

狀態流為 `Planned → Ready → In progress → Evidence ready → Verified`。Ready 表示前置 Gate／介面已可用；Verified 需審查相同 commit 的證據。Blocked 附具體依賴與解除條件；它不等於完成。只有 runtime 驗收成功才填 Verified。

建議證據目錄：`artifacts/<commit>/<profile>/<case-or-subtest>/`，附 manifest、log、遮罩後的識別碼、DB／Broker 對帳及 coverage 原始資料。該路徑是未來 CI 產物約定，不代表檔案目前存在。

### 6.3 相依與範圍變更

Core／Scope Port 先固定再分工；治理資料與業務資料的交界由 API／schema 審查保護。可靠事件與身分／租戶從 M1 接進真路徑，Organization 薄片從 M2 接入，不等 M3 才開始整合。

若變更需求語意，先改實作規範的穩定 ID／細則與案例，再改本表映射、工作包、風險及估算。同一 ID 不因拆 Issue 或延期重編。新增需求沒有正式交付映射、或一項需求被兩個節點聲稱交付時，計畫不得凍結。

## 7. 風險與回歸控制

| 風險 | 契約／案例依據 | 失敗機制 | 回歸控制 | 主要處理節點 |
|---|---|---|---|---|
| 範圍擴增低估 | 規範 §15、A70 | 新模組擠壓安全／測試時間 | M0 重估；每 Sprint 以證據更新餘額 | M0，全程追蹤 |
| UOW 結果誤判 | UOW-020、UOW-021、A05、A33 | 連線失敗被當確定回滾 | Commit 前後斷線、DB 對帳、Outcome 斷言 | M2，M3 擴充整合 |
| 授權／RLS 越界 | TEN-002、SCP-005、A56、A77 | 缺 owner 或保留寬鬆 policy | 多租戶／多 Org、漏 SQL filter、migration 中斷 | M3 |
| 組織治理權變成內容權 | ORG-004、ORG-005、A85、A86 | 父層 allow 穿透 membership | Enterprise owner 無 Org membership 的負測試 | M3 |
| 撤權競態／舊快取 | ORG-007、A88 | Bind 與新 UOW admission 之間撤權 | barrier 控制時序、revision 校驗、原操作對照 | M3 |
| 可選模組變成硬依賴 | ORG-001、ORG-010、A82、A91 | Core import 私有模組、FK 綁死 schema | tenant-only build graph、替換 fixture | M2、M3；M6 發布矩陣 |
| 事件重送或無法路由 | EVT-004、EVT-006、A52、A06 | confirm 誤用、雙 Worker 同時執行 | 真實 Broker 路由失敗、Inbox barrier、崩潰重啟 | M3 |
| 不當卸載暴露資料 | ORG-011、ORG-012、A92、A93 | 刪 Provider 後降級 Tenant 或政策 OR | 啟動拒絕、保留 owner、原子 policy 切換 | M3 |
| Coverage 假達標 | REL-001、A68 | 漏未測 package、錯誤平均、四捨五入 | 原始計數與缺 profile fixture | M1，持續 Gate |
| 組合完成但生成物失敗 | CLI-001、ORG-013、A67、A94 | 範本／Runtime 版本不匹配 | clean runner build／test／啟動 | M5 |
| 依賴過早關閉 | OPS-001、A15 | Drain 中關閉 DB／Broker | 活躍請求與 Worker 停機演練 | M1，M4 擴充負載 |

<a id="release"></a>

## 8. Definition of Done 與發布證據

功能 Done 包含程式碼、契約測試、範例、文件、可觀測性及安全驗證。發布判定引用[實作規範 §13](panGOlin-framework_實作規範書.md#s-13)與[§15](panGOlin-framework_實作規範書.md#s-15)；門檻的正式定義不在此複製。

| 交付物 | 證據入口 | 最終確認角色 |
|---|---|---|
| 公開 Go packages／Runtime／Adapter | 同 commit build、依賴圖、API／契約測試 | 技術負責人 |
| 兩種 Bookstore Profile 與 CLI | 乾淨生成、測試、Compose 啟動紀錄 | Go C、QA |
| 單一 Organization 模組及替換契約 | 三組 Profile matrix、scope／governance 測試 | Go C、QA |
| Schema／Migration／RLS | 空庫／前版／組織啟用／Provider 替換對帳 | Go A、SRE |
| API／事件／Jobs 與冪等 | OpenAPI、Event schema、重送／併發／崩潰報告 | Go A／B、QA |
| 安全／品質證據 | 全部適用案例、coverage 原始計數與 manifest、race／lint／依賴報告 | QA、技術負責人 |
| 操作文件與復原能力 | Runbook 實演、Drain／備份復原、實測 RPO／RTO | SRE |
| 效能與容量 | 硬體、負載、資料量、pool、p95／p99、方法與誤差 | SRE、Go B |
| 發布 manifest | 規範基線、commit、CLI／Runtime／Template 版本、變更／遷移說明 | 技術負責人 |

95 項需求映射、適用案例與證據包一致才完成計畫。此文件不表示已執行實作、外部發布或部署。
