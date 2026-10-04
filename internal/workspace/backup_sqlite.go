package workspace

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"time"

	"github.com/mattn/go-sqlite3"
)

type sqliteBackupSource struct {
	conn *sql.Conn
	tx   *sql.Tx
}

func (s *sqliteBackupSource) Close() {
	if s.tx != nil {
		_ = s.tx.Rollback()
		s.tx = nil
	}
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

// afterStep is an optional observer used by deterministic concurrency/failure
// qualification. Callers must not access either connection from the observer.
func copySQLiteBackup(ctx context.Context, source *sql.Conn, path string, afterStep func() error) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	destination, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: path}).String()+"?mode=rw&_journal_mode=DELETE&_synchronous=FULL&_busy_timeout=50")
	if err != nil {
		return err
	}
	defer destination.Close()
	destination.SetMaxOpenConns(1)
	conn, err := destination.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	err = source.Raw(func(src any) error {
		return conn.Raw(func(dst any) (err error) {
			source, ok := src.(*sqlite3.SQLiteConn)
			if !ok {
				return errors.New("unsupported SQLite backup source driver")
			}
			destination, ok := dst.(*sqlite3.SQLiteConn)
			if !ok {
				return errors.New("unsupported SQLite backup destination driver")
			}
			backup, err := destination.Backup("main", source, "main")
			if err != nil {
				return err
			}
			defer func() {
				if finish := backup.Finish(); err == nil {
					err = finish
				}
			}()
			for {
				if err = ctx.Err(); err != nil {
					return err
				}
				done, stepErr := backup.Step(128)
				if stepErr != nil {
					return stepErr
				}
				if afterStep != nil {
					if err = afterStep(); err != nil {
						return err
					}
				}
				if done {
					return ctx.Err()
				}
				// Yield between bounded copies and back off on SQLite BUSY/LOCKED.
				timer := time.NewTimer(time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
		})
	})
	if err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, "PRAGMA journal_mode=DELETE"); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
