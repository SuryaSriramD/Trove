package workspace

import "context"

type recoveryAttachmentReference struct {
	ImportAttachmentRelationship
	SourceKind string `json:"sourceKind,omitempty"`
}

func recoveryAttachmentKind(r recoveryAttachmentReference) (string, error) {
	kind := r.SourceKind
	if kind == "" {
		kind = attachmentLegacyKind(r.ImportAttachmentRelationship)
	}
	if kind != "explicit" && kind != "explicit_user" && kind != "explicit_block" && kind != "import_evidence" {
		return "", Fail("INVALID_REQUEST", "Unknown historical attachment reference kind")
	}
	if (kind == "explicit_block") != (r.BlockID != "") {
		return "", Fail("INVALID_REQUEST", "Historical attachment kind does not match its block ownership")
	}
	return kind, nil
}

func captureRecoveryAttachments(ctx context.Context, q querier, page string) ([]recoveryAttachmentReference, error) {
	rows, err := q.QueryContext(ctx, "SELECT source_kind,source_id,file_id FROM file_references WHERE page_id=? AND source_kind IN ('explicit','explicit_user','explicit_block','import_evidence') ORDER BY source_kind,source_id,file_id", page)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []recoveryAttachmentReference{}
	for rows.Next() {
		var kind, source, file string
		if err := rows.Scan(&kind, &source, &file); err != nil {
			return nil, err
		}
		r := ImportAttachmentRelationship{FileID: file}
		if kind == "explicit_block" {
			r.BlockID = source
		}
		out = append(out, recoveryAttachmentReference{ImportAttachmentRelationship: r, SourceKind: kind})
	}
	return out, rows.Err()
}
