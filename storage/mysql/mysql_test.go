package mysql

import "testing"

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{}, {DSN: "bad", MaxOpen: 1, MaxIdle: 2}, {DSN: "invalid-dsn", MaxOpen: 1}} {
		if db, err := Open(t.Context(), o); err == nil {
			db.Close()
			t.Fatal("invalid options accepted")
		}
	}
	if _, err := ORM(nil, nil); err == nil {
		t.Fatal("nil pool accepted")
	}
}
