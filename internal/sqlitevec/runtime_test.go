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
		if version != "v0.1.9" {
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

// More than two default chunks, with metadata longer than sqlite-vec's inline
// threshold. This reproduces the upstream DELETE bug missed by tiny probes.
func TestVectorLongMetadataDeleteCompaction(t *testing.T) {
	Auto()
	path := filepath.Join(t.TempDir(), "long-metadata.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	if _, err = db.Exec("CREATE VIRTUAL TABLE vectors USING vec0(embedding float[4], source text)"); err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 3; cycle++ {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		for id := 1; id <= 2305; id++ {
			if _, err = tx.Exec("INSERT INTO vectors(rowid,embedding,source) VALUES(?,vec_f32(?),?)", id, fmt.Sprintf("[%d,1,0,0]", id), fmt.Sprintf("page-identity-%036d-generation-%d", id, cycle)); err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("DELETE FROM vectors WHERE rowid <= 2048"); err != nil {
			t.Fatal("long-metadata deletion failed", err)
		}
		tx, err = db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("DELETE FROM vectors"); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if err = db.Close(); err != nil {
			t.Fatal(err)
		}
		db, err = sql.Open("sqlite3", path)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := db.Query("SELECT rowid,source,distance FROM vectors WHERE embedding MATCH '[2305,1,0,0]' AND k=257 ORDER BY distance")
		if err != nil {
			t.Fatal(err)
		}
		seen := map[int]bool{}
		for rows.Next() {
			var id int
			var source string
			var distance float64
			if err = rows.Scan(&id, &source, &distance); err != nil {
				t.Fatal(err)
			}
			if id <= 2048 || id > 2305 || seen[id] || source != fmt.Sprintf("page-identity-%036d-generation-%d", id, cycle) {
				t.Fatal("deleted or stale metadata returned", id, source)
			}
			seen[id] = true
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if len(seen) != 257 {
			t.Fatal("surviving vectors lost after rollback/reopen", len(seen))
		}
		if _, err = db.Exec("DELETE FROM vectors; VACUUM"); err != nil {
			t.Fatal(err)
		}
		var n int
		if err = db.QueryRow("SELECT count(*) FROM vectors").Scan(&n); err != nil || n != 0 {
			t.Fatal(n, err)
		}
	}
}
