package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func portableSourceKey(kind, id string) string {
	return "trove-" + kind + ":" + strings.ToLower(strings.ReplaceAll(id, "-", ""))
}

type portableMapping struct{ ID, Page, Hash, Revision, IdentityKind, IdentityValue string }

// Portable page and collection renditions can share a filename UUID. Their
// supplied manifest identities, not archive order or matching titles, distinguish
// them. Earlier known-normalizer plans identify the exact rendition mapping.
func retainedPortableMappings(ctx context.Context, q querier, p *ImportPlan) (map[string]portableMapping, error) {
	rows, err := q.QueryContext(ctx, `SELECT m.id,m.local_id,m.base_content_hash,COALESCE(pg.revision,'0'),m.identity_kind,m.identity_value,r.plan_json FROM import_mappings m JOIN import_runs r ON r.id=m.last_import_run_id LEFT JOIN pages pg ON pg.id=m.local_id WHERE m.source_namespace=? AND m.source_system=? AND m.external_kind='page' ORDER BY m.id LIMIT 10001`, p.Namespace, p.SourceSystem)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]portableMapping{}
	count := 0
	for rows.Next() {
		count++
		if count > 10000 {
			return nil, Fail("RESOURCE_EXHAUSTED", "Source mapping inventory exceeds 10000 entries")
		}
		var m portableMapping
		var raw string
		if err = rows.Scan(&m.ID, &m.Page, &m.Hash, &m.Revision, &m.IdentityKind, &m.IdentityValue, &raw); err != nil {
			return nil, err
		}
		var prior ImportPlan
		if json.Unmarshal([]byte(raw), &prior) != nil || prior.NormalizerVersion != importNormalizerVersion {
			continue
		}
		for _, item := range prior.Items {
			if item.MappingID != m.ID || !item.Committed || strings.Contains(item.ExternalID, "#copy=") {
				continue
			}
			key := item.PortableIdentity
			if key == "" && item.Kind == "portable_collection" {
				for _, c := range prior.PortableCollections {
					if c.Locator != item.Locator {
						continue
					}
					for _, identity := range prior.EntityIdentities {
						if identity.Kind == "collection" && identity.ID == c.CollectionID {
							key = portableSourceKey("collection", identity.SourceID)
						}
					}
				}
			} else if key == "" && (item.Kind == "markdown" || item.Kind == "html") {
				kind, id := importIdentity(item.Locator)
				if kind == "external_id" {
					key = portableSourceKey("page", id)
				}
			}
			if key != "" {
				if previous, ok := out[key]; ok && previous.ID != m.ID {
					return nil, Fail("CONFLICT", fmt.Sprintf("Ambiguous retained portable source identity %s; review the prior mappings", key))
				}
				out[key] = m
			}
		}
	}
	return out, rows.Err()
}
