package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/cymondez/pangolin-framework/kernel/di"
)

// 本檔案示範「獨立低階 DI 容器（kernel/di）」，適用於非模組工具、測試或舊式組裝。
// 一般 Root 裝配應用請改用 module.AddSingleton 與 module.Resolve[T]。
type Greeter interface{ Hello(string) string }
type EnglishGreeter struct{}

func (EnglishGreeter) Hello(name string) string { return "Hello, " + name + "!" }

func run() (err error) {
	ctx := context.Background()

	// 步驟 1：為介面建立具型別的 Token Key，並初始化 Registry（須指定正值 Cleanup 逾時）。
	key := di.NewKey[Greeter]("greeter")
	registry, err := di.NewRegistry(time.Second)
	if err != nil {
		return err
	}

	// 步驟 2：註冊 Provider（明確宣告 Key、存活期與 Factory；若有相依鍵須填在 DependsOn）。
	err = registry.Provide(di.Provider{
		Key: key, Lifetime: di.Singleton,
		Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) {
			return EnglishGreeter{}, nil, nil
		},
	})
	if err != nil {
		return err
	}

	// 步驟 3：呼叫 Seal 靜態驗證整張相依圖（檢查缺漏、循環、存活期違規）並建立不可變 Container。
	container, err := registry.Seal(ctx)
	if err != nil {
		return err
	}
	// 步驟 4：由建立者負責在結束時以有界 Cleanup context 關閉 Container，釋放所有資源。
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err = errors.Join(err, container.Close(cleanup))
	}()

	// 步驟 5：透過 Key 解析服務（低階 Container 回傳 any，須手動斷言型別）。
	service, err := container.Resolve(ctx, key)
	if err != nil {
		return err
	}
	fmt.Println(service.(Greeter).Hello("Alice"))
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
