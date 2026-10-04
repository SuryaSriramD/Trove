package workspace

import (
	"context"
	"database/sql"
)

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			var installed int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM service_runtime_migrations WHERE version=20").Scan(&installed); err != nil {
				return err
			}
			if installed != 0 {
				return nil
			}
			// Evidence identities must survive source deletion. Canonical pages and
			// meetings are tombstoned, but blocks/segments can be removed. Live
			// citation validation, rather than these FKs, determines usability.
			_, err := tx.ExecContext(ctx, `
ALTER TABLE memory_sources RENAME TO memory_sources_v1;
CREATE TABLE memory_sources (
 memory_id TEXT NOT NULL REFERENCES memory_entries(id), page_id TEXT NOT NULL REFERENCES pages(id),
 source_revision TEXT NOT NULL, block_id TEXT, meeting_id TEXT REFERENCES meetings(id), segment_id TEXT,
 start_ms INTEGER, end_ms INTEGER, source_index INTEGER NOT NULL CHECK(source_index>=0),
 PRIMARY KEY(memory_id,source_index),
 CHECK((meeting_id IS NULL AND segment_id IS NULL AND start_ms IS NULL AND end_ms IS NULL) OR
 (meeting_id IS NOT NULL AND segment_id IS NOT NULL AND block_id IS NOT NULL AND start_ms>=0 AND end_ms>=start_ms))
);
INSERT INTO memory_sources SELECT * FROM memory_sources_v1;
DROP TABLE memory_sources_v1;
INSERT INTO service_runtime_migrations VALUES(20, strftime('%Y-%m-%dT%H:%M:%fZ','now'));
`)
			return err
		})
	})
}
