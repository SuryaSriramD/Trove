package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"trove.local/phase0/contracts"
)

type taskBinding struct {
	StatusPropertyID string  `json:"statusPropertyId"`
	DuePropertyID    *string `json:"duePropertyId"`
}
type taskDetachChoice struct {
	BlockID string `json:"blockId"`
	Action  string `json:"action"`
}
type taskDetachOccurrence struct {
	BlockID       string `json:"blockId"`
	PageID        string `json:"pageId"`
	PageTitle     string `json:"pageTitle"`
	Trashed       bool   `json:"trashed"`
	Action        string `json:"action"`
	CanRemove     bool   `json:"canRemove"`
	RemovalReason string `json:"removalReason"`
	Text          string `json:"text"`
}
type taskDetachReminder struct {
	ID                string `json:"id"`
	Revision          string `json:"revision"`
	ConfiguredEnabled bool   `json:"configuredEnabled"`
	EligibleBefore    bool   `json:"eligibleBefore"`
	EligibleAfter     bool   `json:"eligibleAfter"`
	Description       string `json:"description"`
}
type taskDetachPlan struct {
	Version           int                                 `json:"version"`
	ID                string                              `json:"id"`
	Hash              string                              `json:"hash"`
	RecordID          string                              `json:"recordId"`
	PageID            string                              `json:"pageId"`
	Title             string                              `json:"title"`
	Binding           taskBinding                         `json:"binding"`
	Reminders         []taskDetachReminder                `json:"reminders"`
	Occurrences       []taskDetachOccurrence              `json:"occurrences"`
	RequiredRevisions []contracts.ServiceExpectedRevision `json:"requiredRevisions"`
}

func init() {
	RegisterQuery("tasks.detachPreview", previewTaskDetach)
	RegisterCommand("task.detach.apply", applyTaskDetach)
	RegisterCommand("task.facet.set", setTaskFacetJournal)
	RegisterCommand("task.occurrence.set", setTaskOccurrenceJournal)
}
func previewTaskDetach(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	var choices []taskDetachChoice
	if err := json.Unmarshal(marshal(r["choices"]), &choices); err != nil {
		return nil, err
	}
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return buildTaskDetachPlan(ctx, k, tx, e, str(r["recordId"]), choices, NewID())
}
func buildTaskDetachPlan(ctx context.Context, k *Kernel, q querier, e contracts.ServiceExecutionContext, record string, choices []taskDetachChoice, id string) (taskDetachPlan, error) {
	out := taskDetachPlan{Version: 1, ID: id, RecordID: record, Occurrences: []taskDetachOccurrence{}, Reminders: []taskDetachReminder{}, RequiredRevisions: []contracts.ServiceExpectedRevision{}}
	if e.Actor.Kind != "user" || e.Origin != "user" {
		return out, Fail("SCOPE_DENIED", "Task detachment requires direct user review")
	}
	if err := k.authorizeEntity(ctx, q, e, "record", record); err != nil {
		return out, err
	}
	var collection string
	var due sql.NullString
	var deleted sql.NullString
	err := q.QueryRowContext(ctx, `SELECT r.page_id,r.collection_id,p.title,p.deleted_at,f.status_property_id,f.due_property_id FROM records r JOIN pages p ON p.id=r.page_id JOIN task_facets f ON f.record_id=r.id WHERE r.id=?`, record).Scan(&out.PageID, &collection, &out.Title, &deleted, &out.Binding.StatusPropertyID, &due)
	if err != nil {
		return out, err
	}
	if deleted.Valid {
		return out, Fail("CONFLICT", "Restore the task before detaching its facet")
	}
	if due.Valid {
		out.Binding.DuePropertyID = &due.String
	}
	guards := map[string]contracts.ServiceExpectedRevision{}
	add := func(kind, id string) error {
		if err := k.authorizeEntity(ctx, q, e, kind, id); err != nil {
			return err
		}
		var revision string
		if err := q.QueryRowContext(ctx, "SELECT revision FROM "+entityTables[kind]+" WHERE id=?", id).Scan(&revision); err != nil {
			return err
		}
		guards[kind+":"+id] = required(kind, id, revision)
		return nil
	}
	addPage := func(page string) error {
		if err := add("page", page); err != nil {
			return err
		}
		var rid, cid string
		err := q.QueryRowContext(ctx, "SELECT id,collection_id FROM records WHERE page_id=?", page).Scan(&rid, &cid)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = add("record", rid); err != nil {
			return err
		}
		return add("collection", cid)
	}
	if err = addPage(out.PageID); err != nil {
		return out, err
	}
	if err = add("property", out.Binding.StatusPropertyID); err != nil {
		return out, err
	}
	if due.Valid {
		if err = add("property", due.String); err != nil {
			return out, err
		}
	}
	var status string
	if err = q.QueryRowContext(ctx, "SELECT o.label FROM property_values v JOIN property_options o ON o.id=v.status_option_id WHERE v.record_id=? AND v.property_id=?", record, out.Binding.StatusPropertyID).Scan(&status); err != nil {
		return out, err
	}
	var completed bool
	if err = q.QueryRowContext(ctx, "SELECT completed FROM task_state WHERE record_id=?", record).Scan(&completed); err != nil {
		return out, err
	}
	reminderRows, err := q.QueryContext(ctx, "SELECT id,revision,json_extract(body_json,'$.enabled'),json_extract(body_json,'$.frequency'),json_extract(body_json,'$.localTime'),json_extract(body_json,'$.timezone') FROM reminder_config_v1 WHERE page_id=? ORDER BY id", out.PageID)
	if err != nil {
		return out, err
	}
	for reminderRows.Next() {
		var r taskDetachReminder
		var frequency, localTime, timezone string
		if err = reminderRows.Scan(&r.ID, &r.Revision, &r.ConfiguredEnabled, &frequency, &localTime, &timezone); err != nil {
			reminderRows.Close()
			return out, err
		}
		r.EligibleBefore = r.ConfiguredEnabled && !completed
		r.EligibleAfter = r.ConfiguredEnabled
		r.Description = frequency + " at " + localTime + " (" + timezone + ")"
		out.Reminders = append(out.Reminders, r)
	}
	err = reminderRows.Err()
	reminderRows.Close()
	if err != nil {
		return out, err
	}
	text := out.Title + " — " + status
	if due.Valid {
		var raw string
		err = q.QueryRowContext(ctx, "SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", record, due.String).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
		if err == nil {
			var d map[string]any
			if err = json.Unmarshal([]byte(raw), &d); err != nil {
				return out, err
			}
			if start := str(d["start"]); start != "" {
				text += "; due " + start
				if end := str(d["end"]); end != "" {
					text += " to " + end
				}
				if zone := str(d["timezone"]); zone != "" {
					text += " (" + zone + ")"
				}
			}
		}
	}
	chosen := map[string]string{}
	for _, choice := range choices {
		if choice.Action != "convert" && choice.Action != "remove" || chosen[choice.BlockID] != "" {
			return out, Fail("INVALID_REQUEST", "Choose conversion or removal once per occurrence")
		}
		chosen[choice.BlockID] = choice.Action
	}
	rows, err := q.QueryContext(ctx, `SELECT b.id,b.page_id,p.title,p.deleted_at,COALESCE(b.parent_block_id,''),
 (SELECT count(*) FROM blocks child WHERE child.parent_block_id=b.id),
 (SELECT count(*) FROM attachments a WHERE a.block_id=b.id)+(SELECT count(*) FROM transcript_segments t WHERE t.block_id=b.id)
 FROM todo_references t JOIN blocks b ON b.id=t.block_id JOIN pages p ON p.id=b.page_id WHERE t.task_record_id=? ORDER BY b.id LIMIT 128`, record)
	if err != nil {
		return out, err
	}
	type occurrenceRow struct {
		item   taskDetachOccurrence
		parent string
	}
	pending := []occurrenceRow{}
	for rows.Next() {
		var x occurrenceRow
		var trashed sql.NullString
		var children, associations int
		if err = rows.Scan(&x.item.BlockID, &x.item.PageID, &x.item.PageTitle, &trashed, &x.parent, &children, &associations); err != nil {
			rows.Close()
			return out, err
		}
		x.item.Trashed = trashed.Valid
		x.item.CanRemove = children == 0 && associations == 0
		x.item.RemovalReason = ""
		if children > 0 {
			x.item.RemovalReason = "Convert this occurrence to preserve its nested blocks."
		}
		if associations > 0 {
			x.item.RemovalReason = "Convert this occurrence to preserve its attached content."
		}
		x.item.Text = text
		x.item.Action = chosen[x.item.BlockID]
		if x.item.Action == "" {
			x.item.Action = "convert"
		}
		delete(chosen, x.item.BlockID)
		pending = append(pending, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(pending) > 127 {
		return out, Fail("RESOURCE_EXHAUSTED", "A reviewed detach supports at most 127 occurrences")
	}
	if len(chosen) > 0 {
		return out, Fail("CONFLICT", "A selected occurrence no longer references this task")
	}
	for _, x := range pending {
		if err = addPage(x.item.PageID); err != nil {
			return out, err
		}
		if err = add("block", x.item.BlockID); err != nil {
			return out, err
		}
		if x.parent != "" {
			if err = add("block", x.parent); err != nil {
				return out, err
			}
		}
		if x.item.Action == "remove" && !x.item.CanRemove {
			return out, Fail("CONFLICT", x.item.RemovalReason)
		}
		out.Occurrences = append(out.Occurrences, x.item)
	}
	keys := []string{}
	for key := range guards {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out.RequiredRevisions = append(out.RequiredRevisions, guards[key])
	}
	out.Hash = snapshotDigest(string(marshal(out)))
	return out, nil
}
func applyTaskDetach(m *Mutation, c map[string]any) error {
	if len(m.Request.Commands) != 1 {
		return Fail("INVALID_REQUEST", "Task detachment requires an isolated reviewed batch")
	}
	var plan taskDetachPlan
	if err := json.Unmarshal(marshal(c["plan"]), &plan); err != nil {
		return err
	}
	choices := []taskDetachChoice{}
	for _, x := range plan.Occurrences {
		choices = append(choices, taskDetachChoice{x.BlockID, x.Action})
	}
	current, err := buildTaskDetachPlan(m.Context, m.Kernel, m.Tx, m.Execution, plan.RecordID, choices, plan.ID)
	if err != nil {
		return err
	}
	if plan.Hash != current.Hash || string(marshal(plan)) != string(marshal(current)) {
		return Fail("CONFLICT", "Task or occurrence state changed; review a new complete plan")
	}
	for _, guard := range current.RequiredRevisions {
		if err = m.Require(guard.Target.EntityType, string(guard.Target.EntityId)); err != nil {
			return err
		}
	}
	policy, err := capturePageExternalContextPolicy(m.Context, m.Tx, plan.PageID)
	if err != nil {
		return err
	}
	actions := []map[string]any{}
	for _, x := range current.Occurrences {
		action := map[string]any{"kind": "task.occurrence.set", "blockId": x.BlockID, "pageId": x.PageID, "state": nil}
		if x.Action == "convert" {
			state, err := taskOccurrenceState(m.Context, m.Tx, x.BlockID, x.PageID)
			if err != nil {
				return err
			}
			state["type"] = "paragraph"
			state["data"] = map[string]any{"richText": []any{map[string]any{"text": x.Text, "link": "trove://page/" + plan.PageID}}}
			action["state"] = state
			if policy != nil {
				if err = retainPageExternalContextPolicy(m.Context, m.Tx, x.PageID, *policy); err != nil {
					return err
				}
			}
		}
		actions = append(actions, action)
	}
	actions = append(actions, map[string]any{"kind": "task.facet.set", "recordId": plan.RecordID, "binding": plan.Binding, "attached": false})
	return applyJournal(m, plan.ID, "Detach task facet: "+strings.TrimSpace(plan.Title), actions)
}
