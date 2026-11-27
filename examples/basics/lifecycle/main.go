package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/cymondez/pangolin-framework/kernel/config"
	"github.com/cymondez/pangolin-framework/kernel/module"
	"github.com/cymondez/pangolin-framework/kernel/runtime"
)

// 本檔案示範「舊式明示清單組裝（runtime.New + Host.Start）」。
// 新專案請優先使用 root-lifecycle 的 app.Run 寫法；本範例供維護既有應用對照。
func run() (err error) {
	// 步驟 1：建立 Host 並明示 Drain / Cleanup 逾時與必要啟動預設設定。
	host, err := runtime.New(runtime.Options{
		DrainTimeout: time.Second, CleanupTimeout: time.Second,
	}, []config.Source{{Name: "defaults", Values: map[string]string{
		"uow.default_timeout": "5s", "uow.cleanup_timeout": "2s",
		"uow.hook_timeout": "1s", "uow.max_requires_new_depth": "4",
	}}})
	if err != nil {
		return err
	}
	// 步驟 2：確保離開時以獨立有界 context 呼叫 host.Stop 反序停機。
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err = errors.Join(err, host.Stop(cleanup))
	}()

	// 步驟 3：以切片明示列出所有模組與字串相依（包含語意版本約束 "=0.1.0"）。
	modules := []module.Module{
		{Name: "storage", Version: "0.1.0",
			Start: func(context.Context, module.Runtime) error { fmt.Println("start storage"); return nil },
			Stop:  func(context.Context, module.Runtime) error { fmt.Println("stop storage"); return nil }},
		{Name: "books", Version: "0.1.0",
			DependsOn: []module.Dependency{{Name: "storage", VersionConstraint: "=0.1.0"}},
			Start:     func(context.Context, module.Runtime) error { fmt.Println("start books"); return nil },
			Stop:      func(context.Context, module.Runtime) error { fmt.Println("stop books"); return nil }},
	}

	// 步驟 4：啟動全部模組；只有全部 Start 成功後，host.Ready() 才會變為 true。
	if err := host.Start(context.Background(), modules); err != nil {
		return err
	}
	fmt.Println("ready:", host.Ready())
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
