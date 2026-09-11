package db

import (
	"database/sql"
	"embed"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

var memSeq atomic.Uint64

func Open(path string) (*sql.DB, error) {
	var dsn string
	if path == ":memory:" {
		// Unique shared-cache memory DB so concurrent queries (HTTP + enforceLoop)
		// share one catalog. Bare ":memory:" is per-connection and needs MaxOpenConns(1),
		// which deadlocks if a Query holds the only conn while a nested QueryRow waits.
		n := memSeq.Add(1)
		dsn = fmt.Sprintf("file:liking-mem-%d?mode=memory&cache=shared&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", n)
	} else {
		if dir := filepath.Dir(path); dir != "" && dir != "." && dir != "/" {
			if err := ensureDir(dir); err != nil {
				return nil, err
			}
		}
		dsn = path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	}
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(8)
	d.SetMaxIdleConns(8)
	if err := d.Ping(); err != nil {
		d.Close()
		return nil, err
	}
	if err := migrate(d); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func migrate(d *sql.DB) error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var dummy string
		err := d.QueryRow(`SELECT version FROM schema_migrations WHERE version = ?`, name).Scan(&dummy)
		if err == nil {
			continue
		}
		if err != sql.ErrNoRows {
			return err
		}
		if err := applyMigration(d, name); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

func applyMigration(d *sql.DB, name string) error {
	body, err := migrations.ReadFile("migrations/" + name)
	if err != nil {
		return err
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(string(body)); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, name, time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
