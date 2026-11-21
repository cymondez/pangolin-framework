// Command m1-query inspects the disposable M1 database for the E2E harness.
package main

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) != 2 || !strings.HasPrefix(os.Args[1], "SELECT ") {
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, e := pgx.Connect(ctx, os.Getenv("PANGOLIN_M1_ADMIN_DSN"))
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	defer conn.Close(ctx)
	tx, e := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	defer tx.Rollback(ctx)
	var result string
	if e = tx.QueryRow(ctx, os.Args[1]).Scan(&result); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	fmt.Println(result)
}
