// 本機教學程式：使用 Bookstore 示範 PostgreSQL 實際觀察 Required 與 RequiresNew 交易傳播行為。
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// 步驟 1：從環境變數取得 Bookstore 示範資料庫連線字串，並建立受控連線池。
	dsn := os.Getenv("PANGOLIN_DSN")
	if dsn == "" {
		return errors.New("set PANGOLIN_DSN after preparing the Bookstore demo")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	// 步驟 2：建立 UOW Manager，明示預設逾時、清理逾時、回呼逾時與 RequiresNew 最大巢狀深度。
	manager, err := uow.NewManager(&postgres.Resource{Pool: pool}, uow.DefaultOptions{
		DefaultTimeout:      5 * time.Second,
		CleanupTimeout:      2 * time.Second,
		HookTimeout:         time.Second,
		MaxRequiresNewDepth: 4,
	})
	if err != nil {
		return err
	}

	// 步驟 3：建立教學用的固定租戶 Scope context。
	// 注意：這只在命令列教學使用；正式 HTTP API 必須由驗證後的 Authn Adapter 提供 IdentityReader。
	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "local-demo", Kind: "user"}, "tenant-a", true
	})
	bound, err := resolver.Bind(ctx, scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	if err != nil {
		return err
	}

	parentID, childID := rand.Text(), rand.Text()
	rollback := errors.New("demonstrate parent rollback")
	var ended context.Context
	var requiredShared, newRoot bool

	// 步驟 4：開啟外層可寫交易（父交易）。
	err = manager.Within(bound, uow.Options{Mode: uow.ReadWrite}, func(parent context.Context) error {
		ended = parent
		root, _ := manager.Current(parent)

		// 4a. 在父交易寫入第一筆書籍（稍後會因父回呼回傳 rollback 錯誤而整筆回滾）。
		if err := writeBook(parent, parentID, "Parent transaction"); err != nil {
			return err
		}

		// 4b. 預設 Propagation 為 Required：加入同一根交易，但降限為 ReadOnly 子視圖。
		// 驗證：子視圖的 ID 與父交易完全相同，且子回呼結束時不會提早 Commit。
		if err := manager.Within(parent, uow.Options{Mode: uow.ReadOnly}, func(child context.Context) error {
			view, _ := manager.Current(child)
			requiredShared = view.ID() == root.ID()
			return nil
		}); err != nil {
			return err
		}

		// 4c. 明示 Propagation: uow.RequiresNew：暫停父範圍，從連線池借第二條連線開獨立交易。
		// 驗證：子範圍擁有全新 Scope ID，寫入第二筆書籍後立刻獨立提交。
		if err := manager.Within(parent, uow.Options{Mode: uow.ReadWrite, Propagation: uow.RequiresNew}, func(child context.Context) error {
			view, _ := manager.Current(child)
			newRoot = view.ID() != root.ID()
			return writeBook(child, childID, "Independent child transaction")
		}); err != nil {
			return err
		}

		// 4d. 父交易刻意回傳錯誤觸發 Rollback，驗證不會撤銷已獨立提交的 RequiresNew 子交易。
		return rollback
	})
	if !errors.Is(err, rollback) {
		return fmt.Errorf("expected rollback: %w", err)
	}

	// 步驟 5：開啟全新的唯讀 UOW 查證資料庫實際留存狀態。
	var parentCount, childCount int
	err = manager.Within(bound, uow.Options{Mode: uow.ReadOnly}, func(work context.Context) error {
		executor, err := postgres.Current(work, false)
		if err != nil {
			return err
		}
		return executor.QueryRow(
			work,
			"SELECT count(*) FILTER(WHERE id=$2),count(*) FILTER(WHERE id=$3) FROM pangolin.books WHERE tenant_id=$1 AND id IN ($2,$3)",
			"tenant-a", parentID, childID,
		).Scan(&parentCount, &childCount)
	})
	if err != nil {
		return err
	}

	// 步驟 6：確認離開 Within 後，先前記下的父 context 已無法再取得作用中的 UOW Scope。
	_, stillActive := manager.Current(ended)
	fmt.Println("Required shares root:", requiredShared)
	fmt.Println("RequiresNew has new root:", newRoot)
	fmt.Println("Parent rolled back:", parentCount == 0)
	fmt.Println("Child committed:", childCount == 1)
	fmt.Println("Scope closed:", !stillActive)
	if !requiredShared || !newRoot || parentCount != 0 || childCount != 1 || stillActive {
		return errors.New("unexpected transaction result")
	}
	return nil
}

// writeBook 示範 Repository 層只從傳入的 ctx 取得受控 Executor 與可信 TenantID，
// 絕不自行呼叫 pool.Begin 或相信外部傳入的 tenant 參數。
func writeBook(ctx context.Context, id, title string) error {
	executor, err := postgres.Current(ctx, true)
	if err != nil {
		return err
	}
	binding, _ := scope.Current(ctx)
	_, err = executor.Exec(ctx, "INSERT INTO pangolin.books(tenant_id,id,title,version) VALUES($1,$2,$3,1)", binding.Ref.TenantID, id, title)
	return err
}
