// Package mysql opens checked SQL pools and optional GORM handles.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	driver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type Options struct {
	DSN         string
	MaxOpen     int
	MaxIdle     int
	MaxLifetime time.Duration
	MaxIdleTime time.Duration
}

// Open transfers pool ownership to the caller, who must Close it.
// No SQL, schema migrations, credentials or retry loops are hidden in this API.
func Open(ctx context.Context, o Options) (*sql.DB, error) {
	if o.MaxOpen <= 0 || o.MaxIdle < 0 || o.MaxIdle > o.MaxOpen || o.MaxLifetime < 0 || o.MaxIdleTime < 0 {
		return nil, errors.New("mysql: invalid pool limits")
	}
	cfg, err := driver.ParseDSN(o.DSN)
	if err != nil {
		return nil, errors.New("mysql: invalid DSN")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 10 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 10 * time.Second
	}
	connector, err := driver.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(o.MaxOpen)
	db.SetMaxIdleConns(o.MaxIdle)
	db.SetConnMaxLifetime(o.MaxLifetime)
	db.SetConnMaxIdleTime(o.MaxIdleTime)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// ORM uses an existing pool. The caller still owns and closes that pool.
// Use returnedDB.WithContext(ctx) for each operation.
func ORM(db *sql.DB, config *gorm.Config) (*gorm.DB, error) {
	if db == nil {
		return nil, errors.New("mysql: nil pool")
	}
	if config == nil {
		config = &gorm.Config{}
	}
	copyConfig := *config
	copyConfig.DisableAutomaticPing = true // pool was already checked by Open
	return gorm.Open(gormmysql.New(gormmysql.Config{Conn: db, SkipInitializeWithVersion: true}), &copyConfig)
}
