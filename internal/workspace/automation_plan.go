package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"trove.local/phase0/contracts"
)

type automationStep struct {
	Command  map[string]any
	Template *templatePlan
}
type automationPrepared struct {
	Commands []map[string]any
	Steps    []automationStep
}

func automationCommands(p *automationPrepared) []map[string]any {
	if p == nil {
		return []map[string]any{}
	}
	return p.Commands
}

// Saved rules are only authored by the owner (automationCommand). A template is
// an immutable direct-user capture, not a reusable AI approval or preview token.
func validateAutomationBindings(ctx context.Context, k *Kernel, tx *sql.Tx, e contracts.ServiceExecutionContext, r AutomationRule) error {
	for _, a := range r.Actions {
		if str(a["kind"]) != "template.instantiate" {
			continue
		}
		if err := contracts.Validate("service", "AutomationTemplateAction", marshal(a)); err != nil {
			return Fail("INVALID_REQUEST", err.Error())
		}
		_, hash, err := loadUnifiedTemplate(ctx, k, tx, e, str(a["templateId"]))
		if err != nil {
			return err
		}
		if hash != str(a["templateHash"]) {
			return Fail("CONFLICT", "Template changed; select the current capture again")
		}
	}
	return nil
}
func automationPreparedGuards(ctx context.Context, tx *sql.Tx, page string, p *automationPrepared) ([]contracts.ServiceExpectedRevision, error) {
	guards, err := automationGuards(ctx, tx, page, automationCommands(p))
	if err != nil {
		return nil, err
	}
	refs := map[string]contracts.ServiceExpectedRevision{}
	for _, g := range guards {
		refs[guardKey(g.Target.EntityType, string(g.Target.EntityId))] = g
	}
	if p != nil {
		for _, step := range p.Steps {
			if step.Template != nil {
				for _, g := range step.Template.Guards {
					key := guardKey(g.Target.EntityType, string(g.Target.EntityId))
					if old, ok := refs[key]; ok && old.Revision != g.Revision {
						return nil, Fail("CONFLICT", "Template and rule guards disagree")
					}
					refs[key] = g
				}
			}
		}
	}
	if len(refs) > 1000 {
		return nil, Fail("RESOURCE_EXHAUSTED", "Automation exceeds 1000 revision guards")
	}
	keys := []string{}
	for key := range refs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []contracts.ServiceExpectedRevision{}
	for _, key := range keys {
		out = append(out, refs[key])
	}
	return out, nil
}
func executeAutomationPlan(m *Mutation, p *automationPrepared) error {
	if p == nil {
		return nil
	}
	for _, step := range p.Steps {
		if step.Template != nil {
			if err := executeTemplatePlan(m, *step.Template); err != nil {
				return err
			}
		} else {
			handler := commandHandlers[str(step.Command["kind"])]
			if handler == nil {
				return Fail("UNSUPPORTED_SCHEMA", "Unknown saved-rule action")
			}
			if err := handler(m, step.Command); err != nil {
				return err
			}
		}
	}
	return nil
}

// Preview exercises exactly the canonical handlers and validators, then rolls
// back every row. It creates no committed events, receipts, or runtime effects.
func validateAutomationPlan(ctx context.Context, k *Kernel, tx *sql.Tx, e contracts.ServiceExecutionContext, page string, p *automationPrepared, guards []contracts.ServiceExpectedRevision) error {
	if _, err := tx.ExecContext(ctx, "SAVEPOINT automation_validation"); err != nil {
		return err
	}
	m := &Mutation{Tx: tx, Kernel: k, Context: ctx, Execution: e, guards: map[string]string{}, touched: map[string]contracts.ServiceRevisionChange{}, before: map[string]json.RawMessage{}}
	for _, g := range guards {
		m.guards[guardKey(g.Target.EntityType, string(g.Target.EntityId))] = g.Revision
	}
	var err error
	for _, a := range p.Commands {
		var c contracts.ServiceCommand
		if err = json.Unmarshal(marshal(a), &c); err != nil {
			break
		}
		m.Request.Commands = append(m.Request.Commands, c)
	}
	if err == nil {
		err = m.TouchPage(page)
	}
	if err == nil {
		err = executeAutomationPlan(m, p)
	}
	if err == nil {
		for _, validate := range commitValidators {
			if err = validate(m); err != nil {
				break
			}
		}
	}
	if _, rollback := tx.ExecContext(ctx, "ROLLBACK TO automation_validation"); rollback != nil {
		return rollback
	}
	if _, release := tx.ExecContext(ctx, "RELEASE automation_validation"); release != nil {
		return release
	}
	return err
}

// Bounded local choices avoid exposing internal event IDs as authoring inputs.
// Only the owner can configure rules; scoped clients receive no metadata here.
func automationOptions(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := contracts.Validate("service", "AutomationOptionsRequest", marshal(r)); err != nil {
		return nil, Fail("INVALID_REQUEST", err.Error())
	}
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = k.authorizePage(ctx, tx, e, str(r["pageId"])); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,name FROM advanced_templates ORDER BY name,id LIMIT 201")
	if err != nil {
		return nil, err
	}
	type item struct{ id, name string }
	items := []item{}
	for rows.Next() {
		var v item
		if err = rows.Scan(&v.id, &v.name); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	templates := []any{}
	for i, v := range items {
		if i == 200 {
			break
		}
		snap, hash, er := loadUnifiedTemplate(ctx, k, tx, e, v.id)
		if er != nil {
			continue
		}
		col := ""
		if snap.Record != nil {
			col = string(snap.Record.CollectionId)
		}
		templates = append(templates, map[string]any{"id": v.id, "name": v.name, "hash": hash, "collectionId": col, "sourcePageId": string(snap.Page.Id)})
	}
	rows, err = tx.QueryContext(ctx, "SELECT event_id,payload_json FROM committed_events ORDER BY cursor DESC LIMIT 200")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []any{}
	for rows.Next() {
		var id, raw string
		if err = rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var ev contracts.ServiceCommittedChangeEvent
		if err = json.Unmarshal([]byte(raw), &ev); err != nil {
			return nil, err
		}
		if ev.Context.Origin != "user" && ev.Context.Origin != "ai" {
			continue
		}
		events = append(events, map[string]any{"id": id, "origin": ev.Context.Origin, "at": ev.CommittedAt, "revisions": ev.Revisions})
	}
	return map[string]any{"templates": templates, "templatesTruncated": len(items) > 200, "events": events}, rows.Err()
}
