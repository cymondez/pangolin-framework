//go:build integration

package designcorrections

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/pangolin-framework/pangolin/application"
	"github.com/pangolin-framework/pangolin/persistence/database"
	"github.com/pangolin-framework/pangolin/persistence/uow"
)

func TestD2ExplicitWriteReturningPortsCommitAndRollback(t *testing.T) {
	rowID, rowRollbackID := "d2-row-"+rand.Text(), "d2-row-rollback-"+rand.Text()
	rowsA, rowsB := "d2-rows-a-"+rand.Text(), "d2-rows-b-"+rand.Text()
	cause := errors.New("deliberate rollback after returning writes")
	f := d1FixtureFor(t, func(context.Context, any) error { return nil })
	if err := application.Command(f.ctx, f.manager, func(ctx context.Context) error {
		ex, err := database.Current(ctx)
		if err != nil {
			return err
		}
		if err := ex.QueryRowWrite(ctx, "INSERT INTO pangolin_design_d1.records(id,value) VALUES($1,$2) RETURNING id", rowID, "row-write").Scan(new(string)); err != nil {
			return err
		}
		rows, err := ex.QueryWrite(ctx, "INSERT INTO pangolin_design_d1.records(id,value) VALUES($1,$2),($3,$4) RETURNING id", rowsA, "rows-write", rowsB, "rows-write")
		if err != nil {
			return err
		}
		defer rows.Close()
		var found int
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			found++
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if found != 2 {
			t.Errorf("QueryWrite returned %d rows, want 2", found)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var committed int
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM pangolin_design_d1.records WHERE id=ANY($1)", []string{rowID, rowsA, rowsB}).Scan(&committed); err != nil {
		t.Fatal(err)
	}
	if committed != 3 {
		t.Fatalf("independent connection saw %d committed write-returning rows, want 3", committed)
	}
	err := application.Command(f.ctx, f.manager, func(ctx context.Context) error {
		ex, err := database.Current(ctx)
		if err != nil {
			return err
		}
		if err := ex.QueryRowWrite(ctx, "INSERT INTO pangolin_design_d1.records(id,value) VALUES($1,$2) RETURNING id", rowRollbackID, "must-rollback").Scan(new(string)); err != nil {
			return err
		}
		return cause
	})
	if !errors.Is(err, cause) {
		t.Fatalf("rollback callback error = %v, want original cause", err)
	}
	var rolledBack int
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM pangolin_design_d1.records WHERE id=$1", rowRollbackID).Scan(&rolledBack); err != nil {
		t.Fatal(err)
	}
	if rolledBack != 0 {
		t.Fatalf("independent connection saw %d rolled-back rows, want 0", rolledBack)
	}
}

func TestD2WriteReturningPortsRejectReadOnlyAndRequiredChild(t *testing.T) {
	readOnlyID, childID := "d2-readonly-"+rand.Text(), "d2-child-"+rand.Text()
	f := d1FixtureFor(t, func(context.Context, any) error { return nil })
	for _, tc := range []struct {
		name string
		work func(context.Context) error
	}{
		{name: "QueryRowWrite on read-only root", work: func(ctx context.Context) error {
			ex, err := database.Current(ctx)
			if err != nil {
				return err
			}
			return ex.QueryRowWrite(ctx, "INSERT INTO pangolin_design_d1.records(id,value) VALUES($1,$2) RETURNING id", readOnlyID, "forbidden").Scan(new(string))
		}},
		{name: "QueryWrite on read-only root", work: func(ctx context.Context) error {
			ex, err := database.Current(ctx)
			if err != nil {
				return err
			}
			_, err = ex.QueryWrite(ctx, "INSERT INTO pangolin_design_d1.records(id,value) VALUES($1,$2) RETURNING id", readOnlyID, "forbidden")
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := application.Query(f.ctx, f.manager, tc.work)
			if !errors.Is(err, uow.ErrReadOnly) {
				t.Fatalf("write-returning on read-only root error = %v, want UOW_READ_ONLY", err)
			}
		})
	}
	err := application.Command(f.ctx, f.manager, func(root context.Context) error {
		childErr := application.Query(root, f.manager, func(child context.Context) error {
			ex, err := database.Current(child)
			if err != nil {
				return err
			}
			if err := ex.QueryRowWrite(child, "INSERT INTO pangolin_design_d1.records(id,value) VALUES($1,$2) RETURNING id", childID, "forbidden").Scan(new(string)); !errors.Is(err, uow.ErrReadOnly) {
				return err
			}
			_, err = ex.QueryWrite(child, "INSERT INTO pangolin_design_d1.records(id,value) VALUES($1,$2) RETURNING id", childID, "forbidden")
			if !errors.Is(err, uow.ErrReadOnly) {
				return err
			}
			return nil
		})
		if childErr != nil {
			t.Errorf("ReadOnly Required child callback itself failed unexpectedly: %v", childErr)
		}
		return nil // Capturing Required failure must not clear root rollback-only.
	})
	if err == nil {
		t.Fatal("ReadWrite root committed after ReadOnly Required write-returning attempts")
	}
	var present int
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM pangolin_design_d1.records WHERE id=ANY($1)", []string{readOnlyID, childID}).Scan(&present); err != nil {
		t.Fatal(err)
	}
	if present != 0 {
		t.Fatalf("independent connection observed %d forbidden write-returning rows", present)
	}
}
