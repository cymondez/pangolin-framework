package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/pangolin-framework/pangolin/kernel/app"
	"github.com/pangolin-framework/pangolin/kernel/config"
	"github.com/pangolin-framework/pangolin/kernel/module"
)

// 步驟 1：定義你自己的業務服務。
// 在同一個模組內直接使用具象 struct 即可，不必急著先抽介面；
// 建構子 NewGreetingService 的精確回傳型別 (*GreetingService) 就是 DI 的解析鍵。
type GreetingService struct{}

func NewGreetingService() *GreetingService { return &GreetingService{} }

func (s *GreetingService) Greet(name string) error {
	_, err := fmt.Fprintf(os.Stdout, "Hello, %s!\n", name)
	return err
}

// 步驟 2：定義功能模組，把「設定預設值」「註冊服務」「啟動執行」收攏在一起。
type greetingModule struct {
	module.Base
}

// 步驟 3：在 Configure 階段宣告「這個模組有哪些預設設定與服務」。
// 注意：Configure 階段只做註冊與設定，此時還不能解析尚未封存的服務。
func (m *greetingModule) Configure(b module.RootBuilder) error {
	// 宣告模組預設設定；若外部提供檔案／環境變數／CLI，會自動覆寫此預設值。
	if err := b.SetDefault("app.name", "World"); err != nil {
		return err
	}
	// 以 Singleton 存活期間註冊建構子，整個應用生命週期共用同一份實例。
	return module.AddSingleton(b.Services(), NewGreetingService)
}

// 步驟 4：在 Start 階段使用已經封存且驗證完成的設定與服務。
func (m *greetingModule) Start(ctx context.Context, rt module.Runtime) error {
	// 從只讀設定取得最終合併後的 app.name（預設值或環境變數覆寫後的結果）。
	name, _ := rt.Config().Lookup("app.name")

	// 使用泛型高階 API module.Resolve[T] 取得具型別服務，完全不需手動轉型。
	service, err := module.Resolve[*GreetingService](ctx, rt.Services())
	if err != nil {
		return err
	}
	return service.Greet(name)
}

func main() {
	// 步驟 5：以 1 秒期限代表一次示範執行（正式服務改用 signal.NotifyContext）。
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()

	// 步驟 6：由應用入口明確決定要讀哪些環境變數，映射成框架設定鍵。
	// 未設定 APP_NAME 時 env 為空 map，會自然退回 Configure 宣告的預設值 "World"。
	env := map[string]string{}
	if name, ok := os.LookupEnv("APP_NAME"); ok {
		env["app.name"] = name
	}

	// 步驟 7：透過 app.Sources 注入 environment 設定層並啟動根模組。
	root := &greetingModule{Base: module.Base{Name: "greeting", Version: "0.1.0"}}
	if err := app.Run(ctx, root, app.Sources(config.Source{Name: "environment", Values: env})); err != nil {
		log.Fatal(err)
	}
}
