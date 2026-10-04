package baseline

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	sqlite3 "github.com/mattn/go-sqlite3"
)

func openDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+path+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil { t.Fatal(err) }
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

// This is a real CGO binary: it verifies bundled SQLite, FTS5, vec registration,
// per-connection pragmas, search/delete/reinsert and the driver's online backup.
func TestPinnedStorageBinary(t *testing.T) {
	sqlitevec.Auto()
	db := openDB(t, filepath.Join(t.TempDir(), "workspace.db"))
	var sqliteVersion, vecVersion, journal string
	if err := db.QueryRow("SELECT sqlite_version(), vec_version()").Scan(&sqliteVersion, &vecVersion); err != nil { t.Fatal(err) }
	t.Logf("sqlite=%s sqlite-vec=%s", sqliteVersion, vecVersion)
	if vecVersion != "v0.1.6" { t.Fatalf("unexpected vec version %q", vecVersion) }
	var foreignKeys int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 { t.Fatalf("foreign keys: %d %v", foreignKeys, err) }
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil || journal != "wal" { t.Fatalf("journal: %q %v", journal, err) }
	for _, stmt := range []string{
		"CREATE TABLE pages(id TEXT PRIMARY KEY, body TEXT NOT NULL)",
		"INSERT INTO pages VALUES('page-1','shared contracts')",
		"CREATE VIRTUAL TABLE page_fts USING fts5(page_id UNINDEXED, body)",
		"INSERT INTO page_fts VALUES('page-1','shared contracts')",
		"CREATE VIRTUAL TABLE embeddings USING vec0(embedding float[3])",
		"INSERT INTO embeddings(rowid,embedding) VALUES(1,'[1,0,0]'),(2,'[0,1,0]')",
	} { if _,err := db.Exec(stmt); err != nil { t.Fatal(err) } }
	var page string
	if err := db.QueryRow("SELECT page_id FROM page_fts WHERE page_fts MATCH ?", "contracts").Scan(&page); err != nil || page != "page-1" { t.Fatalf("fts: %q %v", page, err) }
	nearest := func(expected int) {
		t.Helper(); var id int
		if err := db.QueryRow("SELECT rowid FROM embeddings WHERE embedding MATCH '[1,0,0]' AND k=1").Scan(&id); err != nil || id != expected { t.Fatalf("nearest: %d want %d: %v",id,expected,err) }
	}
	nearest(1)
	if _,err := db.Exec("DELETE FROM embeddings WHERE rowid=1"); err != nil { t.Fatal(err) }
	nearest(2)
	if _,err := db.Exec("INSERT INTO embeddings(rowid,embedding) VALUES(1,'[1,0,0]')"); err != nil { t.Fatal(err) }
	nearest(1)
	backup := openDB(t, filepath.Join(t.TempDir(), "backup.db"))
	ctx := context.Background()
	source,err := db.Conn(ctx); if err != nil { t.Fatal(err) }; defer source.Close()
	dest,err := backup.Conn(ctx); if err != nil { t.Fatal(err) }; defer dest.Close()
	err = dest.Raw(func(d any) error {
		return source.Raw(func(s any) error {
			b,err := d.(*sqlite3.SQLiteConn).Backup("main",s.(*sqlite3.SQLiteConn),"main"); if err != nil { return err }
			done,err := b.Step(-1); if err != nil { b.Finish(); return err }; if !done { t.Fatal("backup incomplete") }
			return b.Finish()
		})
	})
	if err != nil { t.Fatal(err) }
	if err = dest.QueryRowContext(ctx,"SELECT body FROM pages WHERE id='page-1'").Scan(&page); err != nil || page != "shared contracts" { t.Fatalf("backup: %q %v", page,err) }
}
