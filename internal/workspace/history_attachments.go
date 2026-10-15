package workspace

import "context"

func captureRecoveryAttachments(ctx context.Context, q querier, page string) ([]ImportAttachmentRelationship, error) {
	rows, err := q.QueryContext(ctx, "SELECT source_kind,source_id,file_id FROM file_references WHERE page_id=? AND source_kind IN ('explicit','explicit_block') ORDER BY source_kind,source_id,file_id", page)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ImportAttachmentRelationship{}
	for rows.Next() {
		var kind, source, file string
		if err := rows.Scan(&kind, &source, &file); err != nil {
			return nil, err
		}
		r := ImportAttachmentRelationship{FileID: file}
		if kind == "explicit_block" {
			r.BlockID = source
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
