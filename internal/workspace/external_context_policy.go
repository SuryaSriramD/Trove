package workspace

// These restrictions are content provenance, never a capability. They can only
// deny external AI use; imported metadata cannot authorize a provider or tool.
import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"trove.local/phase0/contracts"
)

type importExternalPolicyKey struct{}

// Acquisition evidence is descriptive only. It never restores a credential,
// registration, active account or remote execution authority.
type externalAcquisitionSource struct {
	Version        int    `json:"version"`
	Provider       string `json:"provider"`
	AccountID      string `json:"accountId"`
	SelectionID    string `json:"selectionId"`
	RemoteID       string `json:"remoteId,omitempty"`
	RemoteRevision string `json:"remoteRevision,omitempty"`
}

var externalContextTables = map[string]string{
	"file_external_context_policy":  `CREATE TABLE file_external_context_policy(content_hash TEXT NOT NULL,provider TEXT NOT NULL CHECK(provider='gmail'),version INTEGER NOT NULL CHECK(version=1),policy_json TEXT NOT NULL CHECK(json_valid(policy_json)),PRIMARY KEY(content_hash,provider))`,
	"page_external_context_policy":  `CREATE TABLE page_external_context_policy(page_id TEXT NOT NULL REFERENCES pages(id),provider TEXT NOT NULL CHECK(provider='gmail'),version INTEGER NOT NULL CHECK(version=1),policy_json TEXT NOT NULL CHECK(json_valid(policy_json)),PRIMARY KEY(page_id,provider))`,
	"file_external_context_sources": `CREATE TABLE file_external_context_sources(content_hash TEXT NOT NULL,evidence_hash TEXT NOT NULL,evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json)),PRIMARY KEY(content_hash,evidence_hash))`,
}

type externalContextPolicy struct {
	Version  int    `json:"version"`
	Provider string `json:"provider"`
	Reason   string `json:"reason"`
}

var gmailExternalContextPolicy = externalContextPolicy{Version: 1, Provider: "gmail", Reason: "external_ai_unqualified"}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			var version string
			err := tx.QueryRowContext(ctx, "SELECT value FROM kernel_meta WHERE key='externalContextPolicyVersion'").Scan(&version)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil && version != "1" {
				return Fail("UNSUPPORTED_SCHEMA", "External context policy requires a newer application")
			}
			if err := validateExternalContextRestore(ctx, tx); err != nil {
				return err
			}
			for _, ddl := range externalContextTables {
				if _, err = tx.ExecContext(ctx, strings.Replace(ddl, "CREATE TABLE ", "CREATE TABLE IF NOT EXISTS ", 1)); err != nil {
					return err
				}
			}
			_, err = tx.ExecContext(ctx, "INSERT INTO kernel_meta(key,value) VALUES('externalContextPolicyVersion','1') ON CONFLICT(key) DO NOTHING")
			return err
		})
	})
}
func validateExternalContextPolicy(p externalContextPolicy) error {
	if p != gmailExternalContextPolicy {
		return Fail("UNSUPPORTED_SCHEMA", "External context policy is not supported; preserve its source without weakening restrictions")
	}
	return nil
}
func retainFileExternalContextPolicy(ctx context.Context, tx *sql.Tx, hash string, p externalContextPolicy) error {
	if err := validateExternalContextPolicy(p); err != nil {
		return err
	}
	if !validSHA256(hash) {
		return Fail("INVALID_REQUEST", "External context policy requires a verified content hash")
	}
	_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO file_external_context_policy VALUES(?,?,?,?)", hash, p.Provider, p.Version, string(marshal(p)))
	return err
}
func validSHA256(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, c := range hash {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func fileExternalContextPolicy(ctx context.Context, q querier, hash string) (*externalContextPolicy, error) {
	var raw string
	err := q.QueryRowContext(ctx, "SELECT policy_json FROM file_external_context_policy WHERE content_hash=? AND provider='gmail'", hash).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p externalContextPolicy
	if err = json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	if err = validateExternalContextPolicy(p); err != nil {
		return nil, err
	}
	return &p, nil
}
func pageExternalContextRestricted(ctx context.Context, q querier, page string) (bool, error) {
	// UNION deduplicates the finite page graph, including cyclic memory-source
	// references. Memory pages cannot launder source facts through a new title.
	var restricted int
	err := q.QueryRowContext(ctx, `WITH RECURSIVE evidence(id) AS (
 SELECT ? UNION SELECT ms.page_id FROM evidence e JOIN memory_entries m ON m.page_id=e.id JOIN memory_sources ms ON ms.memory_id=m.id
 ) SELECT EXISTS(SELECT 1 FROM evidence e JOIN pages p ON p.id=e.id WHERE
 EXISTS(SELECT 1 FROM json_tree(p.provenance_json) j WHERE (j.key='sourceSystem' AND j.value='connector:gmail') OR (j.key='provider' AND j.value='gmail')) OR
 EXISTS(SELECT 1 FROM import_mappings m WHERE m.local_id=p.id AND m.source_system='connector:gmail') OR
 EXISTS(SELECT 1 FROM page_external_context_policy policy WHERE policy.page_id=p.id) OR
 EXISTS(SELECT 1 FROM file_references r JOIN files f ON f.id=r.file_id JOIN file_external_context_policy policy ON policy.content_hash=f.content_hash WHERE r.page_id=p.id))`, page).Scan(&restricted)
	return restricted != 0, err
}
func retainPageExternalContextPolicy(ctx context.Context, tx *sql.Tx, page string, p externalContextPolicy) error {
	if err := validateExternalContextPolicy(p); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO page_external_context_policy VALUES(?,?,?,?)", page, p.Provider, p.Version, string(marshal(p)))
	return err
}
func capturePageExternalContextPolicy(ctx context.Context, q querier, page string) (*externalContextPolicy, error) {
	restricted, err := pageExternalContextRestricted(ctx, q, page)
	if err != nil || !restricted {
		return nil, err
	}
	p := gmailExternalContextPolicy
	return &p, nil
}
func retainAIExternalContextPolicy(m *Mutation, sources []AIEvidence) error {
	restricted := false
	for _, s := range sources {
		r, err := pageExternalContextRestricted(m.Context, m.Tx, string(s.Location.PageId))
		if err != nil {
			return err
		}
		restricted = restricted || r
	}
	if !restricted {
		return nil
	}
	// The reviewed batch determines destinations. Its source revision guards do
	// not count as touched pages, so unrelated evidence pages are not marked.
	for _, change := range m.touched {
		if change.Target.EntityType == "page" && !change.Deleted {
			if err := retainPageExternalContextPolicy(m.Context, m.Tx, string(change.Target.EntityId), gmailExternalContextPolicy); err != nil {
				return err
			}
		}
	}
	return nil
}
func externalPolicyProvenance(p *externalContextPolicy, existing any) any {
	if p == nil {
		return existing
	}
	// This portable wrapper survives repeated import nesting. It has no account,
	// credential, activation or execution authority.
	return map[string]any{"externalContextPolicy": p, "supplied": existing}
}

func validateExternalAcquisitionSource(s externalAcquisitionSource) error {
	if s.Version != 1 || s.Provider != "gmail" || contracts.Validate("domain", "Id", marshal(s.AccountID)) != nil || s.SelectionID == "" || len(s.SelectionID) > 8192 || len(s.RemoteID) > 16384 || len(s.RemoteRevision) > 8192 || strings.ContainsRune(s.SelectionID+s.RemoteID+s.RemoteRevision, 0) {
		return Fail("UNSUPPORTED_SCHEMA", "Acquisition provenance is unsupported or exceeds its bounds")
	}
	return nil
}
func retainExternalAcquisitionSource(ctx context.Context, tx *sql.Tx, hash string, source externalAcquisitionSource) error {
	if !validSHA256(hash) {
		return Fail("INTEGRITY_ERROR", "Acquisition source requires verified content identity")
	}
	if err := validateExternalAcquisitionSource(source); err != nil {
		return err
	}
	// Retaining evidence can only strengthen the associated restriction.
	if err := retainFileExternalContextPolicy(ctx, tx, hash, gmailExternalContextPolicy); err != nil {
		return err
	}
	raw := string(marshal(source))
	digest := sha256.Sum256([]byte(raw))
	_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO file_external_context_sources VALUES(?,?,?)", hash, hex.EncodeToString(digest[:]), raw)
	return err
}
func readExternalAcquisitionSources(ctx context.Context, q querier, hash string) ([]externalAcquisitionSource, error) {
	rows, err := q.QueryContext(ctx, "SELECT evidence_json FROM file_external_context_sources WHERE content_hash=? ORDER BY evidence_hash LIMIT 101", hash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []externalAcquisitionSource{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var source externalAcquisitionSource
		if err = json.Unmarshal([]byte(raw), &source); err != nil {
			return nil, err
		}
		if err = validateExternalAcquisitionSource(source); err != nil {
			return nil, err
		}
		out = append(out, source)
	}
	if len(out) > 100 {
		return nil, Fail("RESOURCE_EXHAUSTED", "Asset has too many acquisition sources for portable export")
	}
	return out, rows.Err()
}

// Optional additive tables must be recognized before opening/restoring them.
// Restrictions survive restoration; no mutable connector activation is kept here.
func validateExternalContextRestore(ctx context.Context, tx *sql.Tx) error {
	present := map[string]bool{}
	var version string
	err := tx.QueryRowContext(ctx, "SELECT value FROM kernel_meta WHERE key='externalContextPolicyVersion'").Scan(&version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && version != "1" {
		return Fail("UNSUPPORTED_SCHEMA", "External context policy requires a newer application")
	}
	for name, expected := range externalContextTables {
		var kind, ddl string
		err := tx.QueryRowContext(ctx, "SELECT type,sql FROM sqlite_schema WHERE name=?", name).Scan(&kind, &ddl)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		present[name] = true
		if kind != "table" || normalizeCompatibleDDL(ddl) != normalizeCompatibleDDL(expected) {
			return Fail("UNSUPPORTED_SCHEMA", "External context restore schema is not supported")
		}
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type='trigger' AND tbl_name=? COLLATE NOCASE", name).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return Fail("INTEGRITY_ERROR", "External context restore cannot contain schema triggers")
		}
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+name).Scan(&count); err != nil {
			return err
		}
		if count > 1000000 {
			return Fail("RESOURCE_EXHAUSTED", "External context restore exceeds the supported row limit")
		}
		query := "SELECT " + map[string]string{"file_external_context_policy": "content_hash,provider,version,policy_json", "page_external_context_policy": "page_id,provider,version,policy_json", "file_external_context_sources": "content_hash,evidence_hash,evidence_json"}[name] + " FROM " + name
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		for rows.Next() {
			if name == "file_external_context_sources" {
				var hash, digest, raw string
				err = rows.Scan(&hash, &digest, &raw)
				if err == nil && (!validSHA256(hash) || !validSHA256(digest) || len(raw) > 40000) {
					err = Fail("INTEGRITY_ERROR", "Invalid acquisition provenance identity")
				}
				var s externalAcquisitionSource
				if err == nil {
					err = json.Unmarshal([]byte(raw), &s)
				}
				if err == nil {
					err = validateExternalAcquisitionSource(s)
				}
				sum := sha256.Sum256([]byte(raw))
				if err == nil && digest != hex.EncodeToString(sum[:]) {
					err = Fail("INTEGRITY_ERROR", "Acquisition provenance digest differs")
				}
			} else {
				var id, provider, raw string
				var v int
				err = rows.Scan(&id, &provider, &v, &raw)
				if err == nil && ((name == "file_external_context_policy" && !validSHA256(id)) || (name == "page_external_context_policy" && contracts.Validate("domain", "Id", marshal(id)) != nil) || provider != "gmail" || v != 1 || len(raw) > 1024) {
					err = Fail("INTEGRITY_ERROR", "Invalid external context policy identity")
				}
				var p externalContextPolicy
				if err == nil {
					err = json.Unmarshal([]byte(raw), &p)
				}
				if err == nil {
					err = validateExternalContextPolicy(p)
				}
			}
			if err != nil {
				rows.Close()
				return err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	if present["file_external_context_sources"] {
		var n int
		query := "SELECT COUNT(*) FROM file_external_context_sources"
		if present["file_external_context_policy"] {
			query += " s WHERE NOT EXISTS(SELECT 1 FROM file_external_context_policy p WHERE p.content_hash=s.content_hash AND p.provider='gmail')"
		}
		if err := tx.QueryRowContext(ctx, query).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return Fail("INTEGRITY_ERROR", "Acquisition evidence is missing its external context restriction")
		}
	}
	return nil
}
