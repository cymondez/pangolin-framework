package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/pangolin-framework/pangolin/kernel/app"
	"github.com/pangolin-framework/pangolin/kernel/module"
)

// 步驟 1：定義底層被依賴的 storage 模組（這裡以純印出模擬連線資源）。
type storageModule struct{}

func (m *storageModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "storage", Version: "0.1.0"}
}

func (m *storageModule) Start(context.Context, module.Runtime) error {
	fmt.Println("start storage")
	return nil
}

func (m *storageModule) Stop(context.Context, module.Runtime) error {
	fmt.Println("stop storage")
	return nil
}

// 步驟 2：定義上層業務模組 books，並宣告它直接依賴 storageModule。
type booksModule struct{}

func (m *booksModule) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "books", Version: "0.1.0"}
}

// 步驟 3：透過編譯期型別參照 module.Of[storageModule]() 宣告相依。
// Loader 會自動把 storage 納入相依圖、去除重複相依，並排定：
// 啟動順序 storage → books；停機順序反轉為 books → storage。
func (m *booksModule) DependsOn() []module.Ref {
	return []module.Ref{module.Of[storageModule]()}
}

func (m *booksModule) Start(context.Context, module.Runtime) error {
	fmt.Println("start books")
	return nil
}

func (m *booksModule) Stop(context.Context, module.Runtime) error {
	fmt.Println("stop books")
	return nil
}

func main() {
	// 步驟 4：使用 1 秒逾時模擬收到停機訊號，方便在終端機直接觀察啟動與反序停機。
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()

	// 步驟 5：只需要把最頂層的根模組 booksModule 交給 app.Run；
	// 不用手動組裝模組清單，Loader 會沿著 DependsOn 自動補齊 storageModule。
	if err := app.Run(ctx, &booksModule{}); err != nil {
		log.Fatal(err)
	}
}
