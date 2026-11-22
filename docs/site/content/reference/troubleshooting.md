---
title: 疑難排解
description: 排查安裝、執行範例、登入、租戶與交易的常見問題。
---

## go run 找不到本機套件

確認你位於自己的 `hello-pangolin` 專案，go.mod 的 replace 指向真正的 Framework 資料夾。[安裝頁](../getting-started/installation.md)的兩個資料夾應並排；不要將工作路徑誤設在套件來源裡。

如果找不到範例 main.go，請取得與這份預覽文件相符的來源版本。

## no Go files，或依賴被 tidy 移除

先依[第一個應用](../getting-started/quickstart.mdx)建立 main.go，再執行 go mod tidy。空專案執行 tidy 會移除尚未被 import 的依賴。

## APP_NAME 沒有效果

環境變數只屬於設定它的終端。PowerShell 使用 `$env:APP_NAME = 'Reader'`；macOS／Linux 可用 `APP_NAME=Reader go run .`。執行現成範例時，工作位置應為 examples/basics。

## Bookstore 環境無法啟動

確認 Docker Engine 可連線、Compose 可用，且教學的 ports 沒被占用。`pwsh ./examples/bookstore/scripts/demo.ps1 status` 可查看自己的服務；不要重設共用 Engine 來處理單一應用錯誤。

缺少資料表時，先完成教學的服務準備步驟，再啟動 API。需要重新操作時先停止自己的 API／Worker，再執行 demo.ps1 up；既有書籍資料會保留。

## API 回應 401 或 403

401：Token 可能過期或不正確，本機教學 Token 有效五分鐘，重新執行登入步驟。403：檢查 Tenant Header 是否與登入身份相同，移除自行填入的其他 Scope。

## 交易裡某一步失敗，外層也無法提交

Required 共用同一交易，內層失敗後，外層捕捉錯誤也不會讓部分資料提交。請將整次操作視為失敗，修正問題後才發起新操作。

## 收到 UOW_COMMIT_UNKNOWN

這表示無法確認提交結果。保留原 id，透過查詢／對帳確定狀態，避免直接重送造成重複效果。

## 書籍已建立，背景資料卻沒有更新

確認 Worker 仍在執行，並使用教學中的 stream／subject、worker DB role 與 Tenant 清單。Broker 接受訊息不等於 Consumer 已完成；詳見[事件指南](../events/integration-events.mdx)。
