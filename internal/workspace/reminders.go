package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/reminders"
)

// Activation is bound to this native installation and recovery publication,
// rather than only to a workspace ID that a portable backup preserves.
func (k *Kernel) ReminderRegistrationKey(ctx context.Context, e contracts.ServiceExecutionContext) (string, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return "", err
	}
	var recovery string
	err := k.db.QueryRowContext(ctx, "SELECT value FROM kernel_meta WHERE key=?", recoveryOperationKey).Scan(&recovery)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	sum := sha256.Sum256(marshal([]string{k.Path(), k.ID(), recovery}))
	return hex.EncodeToString(sum[:]), nil
}

type ReminderInboxItem struct {
	reminders.Delivery
	PageID string `json:"pageId"`
	Title  string `json:"title"`
}
type ReminderState struct {
	Rules      []reminders.Rule    `json:"rules"`
	Inbox      []ReminderInboxItem `json:"inbox"`
	More       bool                `json:"more"`
	NextCursor string              `json:"nextCursor,omitempty"`
	Generated  int                 `json:"-"`
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS reminder_config_v1(id TEXT PRIMARY KEY,page_id TEXT NOT NULL REFERENCES pages(id),revision TEXT NOT NULL,body_json TEXT NOT NULL CHECK(json_valid(body_json)),generated_through TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS reminder_history_v1(reminder_id TEXT NOT NULL REFERENCES reminder_config_v1(id),revision TEXT NOT NULL,body_json TEXT NOT NULL,created_at TEXT NOT NULL,PRIMARY KEY(reminder_id,revision));
CREATE TABLE IF NOT EXISTS reminder_inbox_v1(id TEXT PRIMARY KEY,reminder_id TEXT NOT NULL REFERENCES reminder_config_v1(id),revision TEXT NOT NULL,due_at TEXT NOT NULL,outcome TEXT NOT NULL DEFAULT 'scheduled',action TEXT NOT NULL DEFAULT '',action_revision INTEGER NOT NULL DEFAULT 0,snoozed_until TEXT NOT NULL DEFAULT '',error TEXT NOT NULL DEFAULT '');`)
			return err
		})
	})
	RegisterCommand("reminder.put", putReminder)
	RegisterCommand("reminder.inboxAction", reminderInboxAction)
	RegisterQuery("reminders.list", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		return k.remindersPage(ctx, e, time.Now(), str(r["before"]))
	})
	RegisterQuery("reminders.history", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		if err := requireOwnerScope(k, e); err != nil {
			return nil, err
		}
		rows, err := k.db.QueryContext(ctx, "SELECT body_json FROM reminder_history_v1 WHERE reminder_id=? ORDER BY CAST(revision AS INTEGER) DESC LIMIT 100", str(r["reminderId"]))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []reminders.Rule{}
		for rows.Next() {
			var raw string
			var rule reminders.Rule
			if err = rows.Scan(&raw); err != nil {
				return nil, err
			}
			if err = json.Unmarshal([]byte(raw), &rule); err != nil {
				return nil, err
			}
			out = append(out, rule)
		}
		return out, rows.Err()
	})
}
func putReminder(m *Mutation, c map[string]any) error {
	if err := requireOwnerScope(m.Kernel, m.Execution); err != nil {
		return err
	}
	id, page := str(c["reminderId"]), str(c["pageId"])
	var current, oldPage string
	err := m.Tx.QueryRowContext(m.Context, "SELECT revision,page_id FROM reminder_config_v1 WHERE id=?", id).Scan(&current, &oldPage)
	if errors.Is(err, sql.ErrNoRows) {
		current = "0"
	} else if err != nil {
		return err
	}
	if current != str(c["expectedRevision"]) {
		return Fail("CONFLICT", "Reminder changed; reload it before editing")
	}
	if oldPage != "" && oldPage != page {
		return Fail("INVALID_REQUEST", "An existing reminder keeps its source identity")
	}
	revision, err := revNext(current)
	if err != nil {
		return err
	}
	rule := reminders.Rule{ID: id, Revision: revision, PageID: page, StartDate: str(c["startDate"]), LocalTime: str(c["localTime"]), Timezone: str(c["timezone"]), Frequency: str(c["frequency"]), Enabled: c["enabled"] == true, Private: c["private"] == true}
	if err = m.Tx.QueryRowContext(m.Context, "SELECT title FROM pages WHERE id=? AND deleted_at IS NULL", page).Scan(&rule.Title); err != nil {
		return err
	}
	rule.Title = reminderTitle(rule.Title)
	if err = reminders.Validate(rule); err != nil {
		return Fail("INVALID_REQUEST", err.Error())
	}
	var count int
	if err = m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM reminder_config_v1").Scan(&count); err != nil {
		return err
	}
	if current == "0" && count >= 1000 {
		return Fail("RESOURCE_EXHAUSTED", "At most 1000 reminder configurations are supported")
	}
	if err = m.TouchPage(page); err != nil {
		return err
	}
	if _, err = m.Exec("INSERT INTO reminder_config_v1(id,page_id,revision,body_json,generated_through) VALUES(?,?,?,?,'') ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,body_json=excluded.body_json,generated_through=''", id, page, revision, string(marshal(rule))); err != nil {
		return err
	}
	_, err = m.Exec("INSERT INTO reminder_history_v1 VALUES(?,?,?,?)", id, revision, string(marshal(rule)), now())
	return err
}
func reminderInboxAction(m *Mutation, c map[string]any) error {
	if err := requireOwnerScope(m.Kernel, m.Execution); err != nil {
		return err
	}
	var page, action string
	var actionRevision int64
	if err := m.Tx.QueryRowContext(m.Context, "SELECT r.page_id,i.action,i.action_revision FROM reminder_inbox_v1 i JOIN reminder_config_v1 r ON r.id=i.reminder_id WHERE i.id=?", c["occurrenceId"]).Scan(&page, &action, &actionRevision); err != nil {
		return err
	}
	if action != str(c["expectedAction"]) || actionRevision != int64(number(c["expectedActionRevision"])) {
		return Fail("CONFLICT", "This occurrence changed; reload the inbox")
	}
	if err := m.TouchPage(page); err != nil {
		return err
	}
	next := str(c["action"])
	snoozed := ""
	if next == "snoozed" {
		until, err := time.Parse(time.RFC3339, str(c["snoozedUntil"]))
		if err != nil || !until.After(time.Now()) || until.After(time.Now().Add(366*24*time.Hour)) {
			return Fail("INVALID_REQUEST", "Choose a future snooze time within one year")
		}
		snoozed = until.UTC().Format(time.RFC3339)
	} else if next != "dismissed" {
		return Fail("INVALID_REQUEST", "Unsupported reminder inbox action")
	}
	_, err := m.Exec("UPDATE reminder_inbox_v1 SET action=?,snoozed_until=?,action_revision=action_revision+1 WHERE id=?", next, snoozed, c["occurrenceId"])
	return err
}
func reminderTitle(title string) string {
	runes := []rune(title)
	if len(runes) > 128 {
		return string(runes[:128])
	}
	return title
}
func readReminderRules(ctx context.Context, q querier) ([]reminders.Rule, error) {
	rows, err := q.QueryContext(ctx, `SELECT r.body_json,p.title,p.deleted_at,EXISTS(SELECT 1 FROM json_each(prop.config_json,'$.options') opt WHERE json_extract(opt.value,'$.id')=json_extract(val.value_json,'$') AND json_extract(opt.value,'$.group')='done') FROM reminder_config_v1 r JOIN pages p ON p.id=r.page_id LEFT JOIN records rec ON rec.page_id=p.id LEFT JOIN task_facets t ON t.record_id=rec.id LEFT JOIN properties prop ON prop.id=t.status_property_id LEFT JOIN property_values val ON val.record_id=rec.id AND val.property_id=t.status_property_id ORDER BY r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []reminders.Rule{}
	for rows.Next() {
		var raw, title string
		var deleted sql.NullString
		var completed bool
		if err = rows.Scan(&raw, &title, &deleted, &completed); err != nil {
			return nil, err
		}
		var rule reminders.Rule
		if err = json.Unmarshal([]byte(raw), &rule); err != nil {
			return nil, err
		}
		rule.Title = reminderTitle(title)
		if deleted.Valid || completed {
			rule.Enabled = false
		}
		out = append(out, rule)
	}
	return out, rows.Err()
}
func (k *Kernel) Reminders(ctx context.Context, e contracts.ServiceExecutionContext, through time.Time) (ReminderState, error) {
	return k.remindersPage(ctx, e, through, "")
}
func (k *Kernel) remindersPage(ctx context.Context, e contracts.ServiceExecutionContext, through time.Time, before string) (ReminderState, error) {
	out := ReminderState{Rules: []reminders.Rule{}, Inbox: []ReminderInboxItem{}}
	if err := requireOwnerScope(k, e); err != nil {
		return out, err
	}
	err := k.write(ctx, func(tx *sql.Tx) error {
		var err error
		out.Rules, err = readReminderRules(ctx, tx)
		if err != nil {
			return err
		}
		remaining := 1000
		for _, rule := range out.Rules {
			if !rule.Enabled {
				continue
			}
			if remaining == 0 {
				out.More = true
				break
			}
			var cursor string
			if err = tx.QueryRowContext(ctx, "SELECT generated_through FROM reminder_config_v1 WHERE id=?", rule.ID).Scan(&cursor); err != nil {
				return err
			}
			after, _ := time.Parse(time.RFC3339, cursor)
			items, more, err := reminders.Occurrences(rule, after, through, remaining)
			if err != nil {
				return err
			}
			next := through.UTC()
			if more {
				out.More = true
				next, _ = time.Parse(time.RFC3339, items[len(items)-1].DueAt)
			}
			for _, item := range items {
				if _, err = tx.ExecContext(ctx, "INSERT INTO reminder_inbox_v1(id,reminder_id,revision,due_at) VALUES(?,?,?,?) ON CONFLICT DO NOTHING", item.ID, item.ReminderID, item.Revision, item.DueAt); err != nil {
					return err
				}
			}
			out.Generated += len(items)
			remaining -= len(items)
			if _, err = tx.ExecContext(ctx, "UPDATE reminder_config_v1 SET generated_through=? WHERE id=?", next.Format(time.RFC3339), rule.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	var beforeDue string
	if before != "" {
		if err = k.db.QueryRowContext(ctx, "SELECT due_at FROM reminder_inbox_v1 WHERE id=?", before).Scan(&beforeDue); err != nil {
			return out, Fail("INVALID_REQUEST", "Reminder inbox cursor is unavailable")
		}
	}
	rows, err := k.db.QueryContext(ctx, `SELECT i.id,i.reminder_id,i.revision,i.due_at,i.outcome,i.action,i.action_revision,i.snoozed_until,i.error,r.page_id,p.title FROM reminder_inbox_v1 i JOIN reminder_config_v1 r ON r.id=i.reminder_id JOIN pages p ON p.id=r.page_id WHERE p.deleted_at IS NULL AND (?='' OR i.due_at<? OR (i.due_at=? AND i.id<?)) ORDER BY i.due_at DESC,i.id DESC LIMIT 101`, before, beforeDue, beforeDue, before)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var i ReminderInboxItem
		if err = rows.Scan(&i.ID, &i.ReminderID, &i.Revision, &i.DueAt, &i.Outcome, &i.Action, &i.ActionRevision, &i.SnoozedUntil, &i.Error, &i.PageID, &i.Title); err != nil {
			return out, err
		}
		if len(out.Inbox) == 100 {
			out.NextCursor = out.Inbox[99].ID
			break
		}
		out.Inbox = append(out.Inbox, i)
	}
	return out, rows.Err()
}

// ReminderSchedule contains no database paths, credential state or unrelated
// workspace content. The desktop publishes it only after local helper opt-in.
func (k *Kernel) ReminderSchedule(ctx context.Context, e contracts.ServiceExecutionContext) (reminders.Schedule, error) {
	s := reminders.Schedule{Version: 1, WorkspaceID: k.ID(), Rules: []reminders.Rule{}, Overrides: []reminders.Delivery{}}
	if err := requireOwnerScope(k, e); err != nil {
		return s, err
	}
	var err error
	s.Rules, err = readReminderRules(ctx, k.db)
	if err != nil {
		return s, err
	}
	for i := range s.Rules {
		if s.Rules[i].Private {
			s.Rules[i].Title = "Trove reminder"
		}
	}
	rows, err := k.db.QueryContext(ctx, "SELECT id,reminder_id,revision,due_at,outcome,action,action_revision,snoozed_until FROM reminder_inbox_v1 ORDER BY due_at DESC LIMIT 100001")
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var d reminders.Delivery
		if err = rows.Scan(&d.ID, &d.ReminderID, &d.Revision, &d.DueAt, &d.Outcome, &d.Action, &d.ActionRevision, &d.SnoozedUntil); err != nil {
			return s, err
		}
		s.Overrides = append(s.Overrides, d)
	}
	if len(s.Overrides) > 100000 {
		return s, Fail("RESOURCE_EXHAUSTED", "Reminder delivery history exceeds the supported helper publication bound")
	}
	return s, rows.Err()
}

// ImportReminderReceipt accepts only the exact publication identity checked by
// the desktop adapter. Receipts can update delivery evidence, never task values.
func (k *Kernel) ImportReminderReceipt(ctx context.Context, e contracts.ServiceExecutionContext, s reminders.Schedule, r reminders.Receipt) error {
	if err := requireOwnerScope(k, e); err != nil {
		return err
	}
	if r.Version != 1 || r.Epoch != s.Epoch || r.Generation != s.Generation || s.WorkspaceID != k.ID() || len(r.Deliveries) > 100000 {
		return Fail("CONFLICT", "Reminder receipt belongs to an old registration")
	}
	return k.write(ctx, func(tx *sql.Tx) error {
		for _, d := range r.Deliveries {
			if d.Outcome != "scheduled" && d.Outcome != "submitted" && d.Outcome != "failed" && d.Outcome != "unknown" {
				return Fail("INVALID_REQUEST", "Invalid delivery evidence")
			}
			if d.ActionRevision < 0 || d.ActionRevision > 9007199254740991 {
				return Fail("INVALID_REQUEST", "Invalid reminder action revision")
			}
			if d.Action != "" && d.Action != "dismissed" && d.Action != "snoozed" {
				return Fail("INVALID_REQUEST", "Invalid helper action")
			}
			if d.Action == "snoozed" {
				if _, err := time.Parse(time.RFC3339, d.SnoozedUntil); err != nil {
					return Fail("INVALID_REQUEST", "Invalid reminder snooze time")
				}
			} else if d.SnoozedUntil != "" {
				return Fail("INVALID_REQUEST", "Unexpected reminder snooze time")
			}
			due, err := time.Parse(time.RFC3339, d.DueAt)
			if err != nil || d.ID != d.ReminderID+":"+strconv.FormatInt(due.Unix(), 10) || len(d.Error) > 512 {
				return Fail("INVALID_REQUEST", "Invalid reminder occurrence")
			}
			var revision string
			err = tx.QueryRowContext(ctx, "SELECT revision FROM reminder_inbox_v1 WHERE id=?", d.ID).Scan(&revision)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			} else if err != nil {
				return err
			}
			if revision != d.Revision {
				continue
			}
			// A later action wins; equal revisions preserve the canonical action.
			// This permits repeated helper snoozes without replaying old callbacks.
			if _, err = tx.ExecContext(ctx, "UPDATE reminder_inbox_v1 SET outcome=?,error=?,action=CASE WHEN action_revision<? THEN ? ELSE action END,snoozed_until=CASE WHEN action_revision<? THEN ? ELSE snoozed_until END,action_revision=MAX(action_revision,?) WHERE id=?", d.Outcome, d.Error, d.ActionRevision, d.Action, d.ActionRevision, d.SnoozedUntil, d.ActionRevision, d.ID); err != nil {
				return err
			}
		}
		return nil
	})
}
