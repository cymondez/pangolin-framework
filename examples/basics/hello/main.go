package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/pangolin-framework/pangolin/kernel/app"
	"github.com/pangolin-framework/pangolin/kernel/module"
)

// 步驟 1：用普通的 Go struct 定義你的第一個功能模組。
// 嵌入 module.Base 後，就能直接提供模組的 Name 與 Version，
// 不必自己手寫 ModuleMetadata() 方法。
type helloModule struct {
	module.Base
}

// 步驟 2：實作可選的 Start 生命週期方法。
// Runtime 會在所有模組完成設定組裝並驗證無誤後，依相依順序呼叫 Start。
func (m *helloModule) Start(context.Context, module.Runtime) error {
	fmt.Println("Hello, panGOlin!")
	return nil
}

func main() {
	// 步驟 3：建立控制應用存活時間的 context。
	// app.Run 會持續運行直到 ctx 結束；正式長駐服務會改用 signal.NotifyContext
	// 監聽 Ctrl+C / SIGTERM，這裡用 1 秒逾時讓命令列示範在印出後自動優雅停機。
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()

	// 步驟 4：建立根模組實例，交給 app.Run 啟動整個應用。
	// app.Run 會自動組裝預設設定、規劃模組相依圖、啟動模組，並在 ctx 結束時反序 Stop。
	root := &helloModule{Base: module.Base{Name: "hello", Version: "0.1.0"}}
	if err := app.Run(ctx, root); err != nil {
		log.Fatal(err)
	}
}
