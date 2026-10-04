package sqlitevec

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestPinnedRuntimeRegistersNewConnections(t *testing.T) {
	Auto()
	for range 2 {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		var version string
		if err = db.QueryRow("SELECT vec_version()").Scan(&version); err != nil {
			t.Fatal(err)
		}
		if version != "v0.1.6" {
			t.Fatalf("unexpected pinned runtime %q", version)
		}
		if err = db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVectorRuntimeUpdateDeleteReopenStress(t *testing.T) {
	Auto()
	path := filepath.Join(t.TempDir(), "vectors.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.Exec("CREATE VIRTUAL TABLE evidence_vectors USING vec0(embedding float[4])"); err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 8; cycle++ {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		for id := 1; id <= 256; id++ {
			vector := fmt.Sprintf("[%d,0,0,0]", id)
			if _, err = tx.Exec("INSERT INTO evidence_vectors(rowid,embedding) VALUES (?,vec_f32(?))", id, vector); err != nil {
				_ = tx.Rollback()
				t.Fatal(err)
			}
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("DELETE FROM evidence_vectors WHERE rowid % 2 = 0"); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("UPDATE evidence_vectors SET embedding=vec_f32('[0,1,0,0]') WHERE rowid % 2 = 1"); err != nil {
			t.Fatal(err)
		}
		rows, err := db.Query("SELECT rowid,distance FROM evidence_vectors WHERE embedding MATCH '[0,1,0,0]' AND k=10")
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for rows.Next() {
			var id int
			var distance float64
			if err = rows.Scan(&id, &distance); err != nil {
				t.Fatal(err)
			}
			if id%2 != 1 || distance != 0 {
				t.Fatalf("stale/deleted vector returned: id=%d distance=%f", id, distance)
			}
			found++
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
		if found != 10 {
			t.Fatalf("got %d matches", found)
		}
		tx, err = db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("DELETE FROM evidence_vectors"); err != nil {
			t.Fatal(err)
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		var count int
		if err = db.QueryRow("SELECT count(*) FROM evidence_vectors").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 128 {
			t.Fatalf("rollback lost vectors: %d", count)
		}
		if _, err = db.Exec("DELETE FROM evidence_vectors"); err != nil {
			t.Fatal(err)
		}
		if err = db.Close(); err != nil {
			t.Fatal(err)
		}
		db, err = sql.Open("sqlite3", path)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.QueryRow("SELECT count(*) FROM evidence_vectors").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("deleted vectors returned after reopen: %d", count)
		}
	}
}
