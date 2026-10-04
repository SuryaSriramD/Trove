package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"trove.local/phase0/contracts"
)

// RunAutomationTick is called by the desktop host while the workspace is open.
// It has no daemon, remote effects, or permission bypass: every effect commits
// through a guarded Batch built from the saved, user-enabled local rule.
func (k *Kernel) RunAutomationTick(ctx context.Context, e contracts.ServiceExecutionContext, at time.Time) error {
	if err := requireOwnerScope(k, e); err != nil {
		return err
	}
	if e.Origin != "user" {
		return Fail("SCOPE_DENIED", "Only the local owner runtime may execute saved automations")
	}
	rows, err := k.db.QueryContext(ctx, "SELECT id FROM automation_rules ORDER BY id LIMIT 1000")
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err = ctx.Err(); err != nil {
			return err
		}
		s, err := loadAutomation(ctx, k.db, id)
		if err != nil {
			return err
		}
		if !s.Rule.Enabled || s.Rule.Paused {
			continue
		}
		if s.Rule.Trigger.Type == "schedule" {
			if err = k.tickSchedule(ctx, e, s, at); err != nil {
				return err
			}
		} else if err = k.tickEvents(ctx, e, s, at); err != nil {
			return err
		}
	}
	return nil
}
func (k *Kernel) tickEvents(ctx context.Context, e contracts.ServiceExecutionContext, s automationStored, at time.Time) error {
	rows, err := k.db.QueryContext(ctx, "SELECT cursor,event_id FROM committed_events WHERE cursor>? ORDER BY cursor LIMIT 200", s.Cursor)
	if err != nil {
		return err
	}
	type event struct {
		cursor int64
		id     string
	}
	events := []event{}
	for rows.Next() {
		var ev event
		if err = rows.Scan(&ev.cursor, &ev.id); err != nil {
			rows.Close()
			return err
		}
		events = append(events, ev)
	}
	rows.Close()
	for _, event := range events {
		key := "event:" + event.id
		if err = k.executeAutomationOccurrence(ctx, e, s, event.id, key, at); err != nil {
			return err
		}
		if err = k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE automation_rules SET event_cursor=MAX(event_cursor,?) WHERE id=? AND version=?", event.cursor, s.Rule.ID, s.Version)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}
func (k *Kernel) tickSchedule(ctx context.Context, e contracts.ServiceExecutionContext, s automationStored, at time.Time) error {
	next, err := time.Parse(time.RFC3339Nano, s.Next)
	if err != nil {
		return err
	}
	if next.After(at) {
		return nil
	}
	t := s.Rule.Trigger
	handled := 0
	for !next.After(at) {
		key := "schedule:" + next.UTC().Format(time.RFC3339Nano)
		missed := at.Sub(next) > time.Minute
		if missed && (t.MissedPolicy == "skip" || handled >= t.CatchUpLimit) {
			if err = k.logAutomation(ctx, s, key, "skipped", "Missed while Trove was not running; skipped by the configured policy", nil); err != nil {
				return err
			}
			next, err = NextAutomationOccurrence(t, at)
			if err != nil {
				return err
			}
			break
		}
		if err = k.executeAutomationOccurrence(ctx, e, s, "", key, next); err != nil {
			return err
		}
		handled++
		next, err = NextAutomationOccurrence(t, next)
		if err != nil {
			return err
		}
		if handled >= 20 && !next.After(at) {
			if err = k.logAutomation(ctx, s, "schedule:"+next.UTC().Format(time.RFC3339Nano), "skipped", "Remaining missed occurrences skipped after bounded catch-up", nil); err != nil {
				return err
			}
			next, err = NextAutomationOccurrence(t, at)
			if err != nil {
				return err
			}
			break
		}
	}
	return k.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE automation_rules SET next_at=? WHERE id=? AND version=?", next.UTC().Format(time.RFC3339Nano), s.Rule.ID, s.Version)
		return err
	})
}
func (k *Kernel) executeAutomationOccurrence(ctx context.Context, e contracts.ServiceExecutionContext, s automationStored, event, key string, at time.Time) error {
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	current, err := loadAutomation(ctx, tx, s.Rule.ID)
	if err != nil {
		tx.Rollback()
		return err
	}
	if current.Version != s.Version || !current.Rule.Enabled || current.Rule.Paused {
		tx.Rollback()
		return nil
	}
	var status string
	err = tx.QueryRowContext(ctx, "SELECT status FROM automation_runs WHERE rule_id=? AND rule_version=? AND occurrence_key=?", s.Rule.ID, s.Version, key).Scan(&status)
	if err == nil && status != "failed" {
		tx.Rollback()
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		tx.Rollback()
		return err
	}
	actions, eligible, why, err := prepareAutomation(ctx, k, tx, e, current, event, key, at)
	if err != nil {
		tx.Rollback()
		return k.logAutomation(ctx, s, key, "failed", err.Error(), nil)
	}
	if !eligible {
		tx.Rollback()
		return k.logAutomation(ctx, s, key, "skipped", why, nil)
	}
	guards, err := automationPreparedGuards(ctx, tx, s.Page, actions)
	tx.Rollback()
	if err != nil {
		return k.logAutomation(ctx, s, key, "failed", err.Error(), automationCommands(actions))
	}
	command := map[string]any{"kind": "automation.run", "pageId": s.Page, "ruleId": s.Rule.ID, "occurrenceKey": key, "at": at.UTC().Format(time.RFC3339Nano)}
	if event != "" {
		command["eventId"] = event
	}
	// A fresh request can safely retry a failed attempt. The persisted occurrence
	// key is checked inside the same transaction as effects, even across restarts.
	var req contracts.ServiceBatchRequest
	raw := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.id, "mutation": map[string]any{"scopeGrantId": e.ScopeGrantId, "idempotencyKey": NewID(), "expectedRevisions": guards}, "commands": []any{command}}
	json.Unmarshal(marshal(raw), &req)
	response := k.Batch(ctx, e, req)
	var result map[string]any
	json.Unmarshal(marshal(response), &result)
	if result["ok"] != true {
		diagnostic := string(marshal(result["error"]))
		return k.logAutomation(ctx, s, key, "failed", diagnostic, automationCommands(actions))
	}
	return nil
}
func (k *Kernel) logAutomation(ctx context.Context, s automationStored, key, status, diagnostic string, actions any) error {
	if len(diagnostic) > 4000 {
		diagnostic = diagnostic[:4000]
	}
	return k.write(ctx, func(tx *sql.Tx) error {
		// Never downgrade a successful or skipped occurrence on a racing retry.
		result, err := tx.ExecContext(ctx, "INSERT INTO automation_runs(id,rule_id,rule_version,occurrence_key,status,diagnostic,created_at,commands_json) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(rule_id,rule_version,occurrence_key) DO UPDATE SET diagnostic=excluded.diagnostic,created_at=excluded.created_at WHERE automation_runs.status='failed'", NewID(), s.Rule.ID, s.Version, key, status, diagnostic, now(), string(marshal(actions)))
		if err != nil {
			return err
		}
		changed, _ := result.RowsAffected()
		if status == "failed" && changed > 0 {
			current, err := loadAutomation(ctx, tx, s.Rule.ID)
			if err != nil {
				return err
			}
			current.Failures++
			if current.Failures >= 3 {
				current.Rule.Paused = true
			}
			_, err = tx.ExecContext(ctx, "UPDATE automation_rules SET failures=?,rule_json=? WHERE id=? AND version=?", current.Failures, string(marshal(current.Rule)), s.Rule.ID, s.Version)
			return err
		}
		return nil
	})
}

// RetryAutomation uses the original occurrence identity, so a lost response can
// never duplicate a previously committed local effect.
func (k *Kernel) RetryAutomation(ctx context.Context, e contracts.ServiceExecutionContext, ruleID, key string) error {
	if err := requireOwnerScope(k, e); err != nil {
		return err
	}
	s, err := loadAutomation(ctx, k.db, ruleID)
	if err != nil {
		return err
	}
	if s.Rule.Paused || !s.Rule.Enabled {
		return Fail("INVALID_REQUEST", "Resume and enable the rule before retrying")
	}
	var status string
	if err = k.db.QueryRowContext(ctx, "SELECT status FROM automation_runs WHERE rule_id=? AND rule_version=? AND occurrence_key=?", ruleID, s.Version, key).Scan(&status); err != nil {
		return err
	}
	if status != "failed" {
		return nil
	}
	event := ""
	at := time.Now().UTC()
	if strings.HasPrefix(key, "schedule:") {
		at, err = time.Parse(time.RFC3339Nano, strings.TrimPrefix(key, "schedule:"))
		if err != nil {
			return err
		}
	} else if strings.HasPrefix(key, "event:") {
		event = strings.TrimPrefix(key, "event:")
	} else {
		return fmt.Errorf("invalid occurrence key")
	}
	s.Cursor = -1 // retry eligibility must be checked against the retained event, not the processed cursor
	return k.executeAutomationOccurrence(ctx, e, s, event, key, at)
}

// executePreviewedAutomation preserves the revision guards shown in the UI and
// records failure diagnostics after Batch has rolled all canonical effects back.
func executePreviewedAutomation(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, input map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	s, err := loadAutomation(ctx, k.db, str(input["ruleId"]))
	if err != nil {
		return nil, err
	}
	if s.Page != str(input["pageId"]) {
		return nil, Fail("SCOPE_DENIED", "Rule page mismatch")
	}
	if err = k.authorizePage(ctx, k.db, e, s.Page); err != nil {
		return nil, err
	}
	var guards []contracts.ServiceExpectedRevision
	if err = json.Unmarshal(marshal(input["expectedRevisions"]), &guards); err != nil || len(guards) == 0 {
		return nil, Fail("INVALID_REQUEST", "A reviewed run requires its preview revision guards")
	}
	command := map[string]any{"kind": "automation.run", "pageId": s.Page, "ruleId": s.Rule.ID, "occurrenceKey": input["occurrenceKey"], "at": input["at"]}
	if event := str(input["eventId"]); event != "" {
		command["eventId"] = event
	}
	raw := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.id, "mutation": map[string]any{"scopeGrantId": e.ScopeGrantId, "idempotencyKey": NewID(), "expectedRevisions": guards}, "commands": []any{command}}
	var req contracts.ServiceBatchRequest
	if err = json.Unmarshal(marshal(raw), &req); err != nil {
		return nil, Fail("INVALID_REQUEST", err.Error())
	}
	response := k.Batch(ctx, e, req)
	var result map[string]any
	json.Unmarshal(marshal(response), &result)
	if result["ok"] == true {
		return result["result"], nil
	}
	failure, _ := result["error"].(map[string]any)
	if err = k.logAutomation(ctx, s, str(input["occurrenceKey"]), "failed", str(failure["message"]), nil); err != nil {
		return nil, err
	}
	return nil, Fail(str(failure["code"]), str(failure["message"]))
}
