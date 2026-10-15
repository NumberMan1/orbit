//go:build integration

package mysql

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestMySQLIntegration(t *testing.T) {
	dsn := os.Getenv("ORBIT_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set ORBIT_TEST_MYSQL_DSN to an isolated test database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	db, err := Open(ctx, Options{DSN: dsn, MaxOpen: 4, MaxIdle: 2, MaxLifetime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if db.Stats().MaxOpenConnections != 4 {
		t.Fatal("pool maximum not applied")
	}
	var n int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&n); err != nil || n != 1 {
		t.Fatalf("SQL: %d %v", n, err)
	}
	orm, err := ORM(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := orm.WithContext(ctx).Raw("SELECT 2").Scan(&n).Error; err != nil || n != 2 {
		t.Fatalf("ORM: %d %v", n, err)
	}
}
