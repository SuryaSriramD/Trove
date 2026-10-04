-- Apply after workspace.sql. Canonical service-owned metadata; not page/record bodies.
PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS mutation_history (
 history_id TEXT PRIMARY KEY, transaction_id TEXT NOT NULL UNIQUE,
 request_id TEXT NOT NULL, actor_id TEXT NOT NULL, origin TEXT NOT NULL
 CHECK(origin IN ('user','ai','external','import','restore','automation','system','migration')),
 scope_json TEXT NOT NULL CHECK(json_valid(scope_json)), policy_revision INTEGER NOT NULL CHECK(typeof(policy_revision)='integer' AND policy_revision BETWEEN 1 AND 9223372036854775807),
 approval_id TEXT, command_hash TEXT NOT NULL CHECK(length(command_hash)=64),
 before_json TEXT NOT NULL CHECK(json_valid(before_json)), after_json TEXT NOT NULL CHECK(json_valid(after_json)),
 committed_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS committed_events (
 cursor INTEGER PRIMARY KEY AUTOINCREMENT,
 event_id TEXT NOT NULL UNIQUE, transaction_id TEXT NOT NULL UNIQUE REFERENCES mutation_history(transaction_id),
 payload_json TEXT NOT NULL CHECK(json_valid(payload_json))
);
CREATE TABLE IF NOT EXISTS idempotency_receipts (
 workspace_id TEXT NOT NULL, actor_id TEXT NOT NULL, idempotency_key TEXT NOT NULL,
 command_hash TEXT NOT NULL CHECK(length(command_hash)=64),
 transaction_id TEXT NOT NULL REFERENCES mutation_history(transaction_id),
 response_json TEXT NOT NULL CHECK(json_valid(response_json)),
 PRIMARY KEY(workspace_id,actor_id,idempotency_key)
);
CREATE TABLE IF NOT EXISTS approvals (
 approval_id TEXT PRIMARY KEY, actor_id TEXT NOT NULL, scope_grant_id TEXT NOT NULL,
 policy_revision INTEGER NOT NULL CHECK(typeof(policy_revision)='integer' AND policy_revision BETWEEN 1 AND 9223372036854775807), command_hash TEXT NOT NULL CHECK(length(command_hash)=64),
 expected_revisions_json TEXT NOT NULL CHECK(json_valid(expected_revisions_json)),
 expires_at TEXT NOT NULL, approved_by TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','approved','consumed','revoked','expired'))
);
CREATE TABLE IF NOT EXISTS jobs (
 job_id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id), kind TEXT NOT NULL,
 payload_version INTEGER NOT NULL CHECK(payload_version=1), payload_json TEXT NOT NULL CHECK(json_valid(payload_json)),
 state TEXT NOT NULL CHECK(state IN ('queued','running','retry_wait','cancel_requested','cancelled','succeeded','failed','interrupted')),
 revision INTEGER NOT NULL CHECK(typeof(revision)='integer' AND revision BETWEEN 1 AND 9223372036854775807), idempotency_key TEXT NOT NULL,
 attempt INTEGER NOT NULL DEFAULT 0 CHECK(attempt BETWEEN 0 AND 5),
 max_attempts INTEGER NOT NULL CHECK(max_attempts BETWEEN 1 AND 5),
 lease_token TEXT, lease_expires_at TEXT, next_attempt_at TEXT, checkpoint_ref TEXT,
 progress_json TEXT CHECK(progress_json IS NULL OR json_valid(progress_json)),
 error_json TEXT CHECK(error_json IS NULL OR json_valid(error_json)), created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 UNIQUE(workspace_id,kind,idempotency_key),
 CHECK(attempt<=max_attempts),
 CHECK((state IN ('running','cancel_requested') AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL)
    OR (state NOT IN ('running','cancel_requested') AND lease_token IS NULL AND lease_expires_at IS NULL))
);
CREATE TABLE IF NOT EXISTS job_results (
 job_id TEXT NOT NULL REFERENCES jobs(job_id), unit_key TEXT NOT NULL,
 transaction_id TEXT NOT NULL REFERENCES mutation_history(transaction_id), result_hash TEXT NOT NULL,
 PRIMARY KEY(job_id,unit_key)
);
