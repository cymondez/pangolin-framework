package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/pangolin-framework/pangolin/kernel/config"
	"github.com/pangolin-framework/pangolin/kernel/di"
	"github.com/pangolin-framework/pangolin/kernel/module"
	"github.com/pangolin-framework/pangolin/kernel/runtime"
)

// 本檔案示範「舊式明示清單組裝（runtime.New + Host.Start）」。
// 新專案請優先使用 root-greeting 的 app.Run 寫法；本範例供維護既有應用對照。
type GreetingService struct{ output io.Writer }

func (s *GreetingService) Greet(name string) error {
	_, err := fmt.Fprintf(s.output, "Hello, %s!\n", name)
	return err
}

func run() (err error) {
	// 步驟 1：手動讀取環境變數，並與 defaults 設定層一起交給 runtime.New。
	environment := map[string]string{}
	if name, ok := os.LookupEnv("APP_NAME"); ok {
		environment["app.name"] = name
	}
	host, err := runtime.New(runtime.Options{
		DrainTimeout: time.Second, CleanupTimeout: time.Second,
	}, []config.Source{
		// 明示清單組裝需手動補齊 uow.* 必要啟動預設值（app.Run 則已內建）。
		{Name: "defaults", Values: map[string]string{
			"app.name": "World", "uow.default_timeout": "5s",
			"uow.cleanup_timeout": "2s", "uow.hook_timeout": "1s",
			"uow.max_requires_new_depth": "4",
		}},
		{Name: "environment", Values: environment},
	})
	if err != nil {
		return err
	}
	// 步驟 2：離開函式前使用獨立的 Cleanup context 呼叫 host.Stop，
	// 並用 errors.Join 確保停機清理錯誤不會蓋掉主要錯誤。
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err = errors.Join(err, host.Stop(cleanup))
	}()

	// 步驟 3：以結構值 module.Module 定義模組與低階 di.Provider 註冊。
	key := module.Service[*GreetingService]()
	app := module.Module{
		Name: "greeting", Version: "0.1.0",
		Configure: func(b module.Builder) error {
			return b.Provide(di.Provider{
				Key: key, Lifetime: di.Singleton,
				Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
					return &GreetingService{output: os.Stdout}, nil, nil
				},
			})
		},
		Start: func(ctx context.Context, app module.Runtime) error {
			name, _ := app.Config().Lookup("app.name")
			service, err := module.Resolve[*GreetingService](ctx, app.Services())
			if err != nil {
				return err
			}
			return service.Greet(name)
		},
	}
	// 步驟 4：將模組切片明示交給 host.Start 啟動。
	return host.Start(context.Background(), []module.Module{app})
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
