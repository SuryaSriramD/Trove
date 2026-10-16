package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"trove.local/phase0/contracts"
	"unicode/utf8"
)

// Image descriptions are derived drafts, excluded from canonical snapshots,
// search, memories, portable imports and all command/proposal write paths.
var imageDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type ImageDescription struct {
	PreprocessingProfileID   string `json:"preprocessingProfileId,omitempty"`
	PreprocessingProfileHash string `json:"preprocessingProfileHash,omitempty"`
	PeakRSSBytes             int64  `json:"sampledPeakRSSBytes,omitempty"`
	AnalysisMethod           string `json:"analysisMethod,omitempty"`
	ID                       string `json:"id"`
	PageID                   string `json:"pageId"`
	FileID                   string `json:"fileId"`
	ModelPackID              string `json:"modelPackId"`
	ActorID                  string `json:"-"`
	State                    string `json:"state"`
	Question                 string `json:"question"`
	Text                     string `json:"text"`
	PageRevision             string `json:"pageRevision"`
	FileRevision             string `json:"fileRevision"`
	ContentHash              string `json:"contentHash"`
	ModelHash                string `json:"modelHash,omitempty"`
	ProjectorHash            string `json:"projectorHash,omitempty"`
	HelperHash               string `json:"helperHash,omitempty"`
	InputTokens              int    `json:"inputTokens,omitempty"`
	OutputTokens             int    `json:"outputTokens,omitempty"`
	FinishReason             string `json:"finishReason,omitempty"`
	CreatedAt                string `json:"createdAt"`
	Error                    string `json:"error,omitempty"`
	Warning                  string `json:"warning"`
	Stale                    bool   `json:"stale"`
}
type ImageDescriptionResult struct {
	Text, ModelHash, ProjectorHash, HelperHash, FinishReason, AnalysisMethod string
	PreprocessingProfileID, PreprocessingProfileHash                         string
	PeakRSSBytes                                                             int64
	InputTokens, OutputTokens                                                int
}
type ImageDescriptionGenerator func(context.Context, string, string, *os.File) (ImageDescriptionResult, error)

func imageOwner(k *Kernel, e contracts.ServiceExecutionContext) error {
	if e.Origin != "user" || e.Actor.Kind != "user" || string(e.Actor.ActorId) != k.ownerID {
		return Fail("SCOPE_DENIED", "Image analysis requires a direct workspace-owner request")
	}
	return nil
}
func readImageDescription(ctx context.Context, q querier, id string) (ImageDescription, error) {
	var r ImageDescription
	var raw string
	err := q.QueryRowContext(ctx, "SELECT actor_id,payload_json FROM image_descriptions WHERE id=?", id).Scan(&r.ActorID, &raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &r)
	}
	return r, err
}
func storeImageDescription(ctx context.Context, tx *sql.Tx, r ImageDescription) error {
	_, err := tx.ExecContext(ctx, "UPDATE image_descriptions SET state=?,payload_json=? WHERE id=?", r.State, string(marshal(r)), r.ID)
	return err
}
func (k *Kernel) imageSourceCurrent(ctx context.Context, q querier, e contracts.ServiceExecutionContext, r ImageDescription) error {
	if err := imageOwner(k, e); err != nil {
		return err
	}
	if r.ActorID != string(e.Actor.ActorId) {
		return Fail("SCOPE_DENIED", "Image draft belongs to another actor")
	}
	if err := k.authorizePage(ctx, q, e, r.PageID); err != nil {
		return err
	}
	if err := k.authorizeEntity(ctx, q, e, "file", r.FileID); err != nil {
		return err
	}
	var pageRev, fileRev, hash string
	err := q.QueryRowContext(ctx, "SELECT p.revision,f.revision,f.content_hash FROM pages p JOIN attachments a ON a.page_id=p.id JOIN files f ON f.id=a.file_id WHERE p.id=? AND f.id=? AND p.deleted_at IS NULL", r.PageID, r.FileID).Scan(&pageRev, &fileRev, &hash)
	if err != nil || pageRev != r.PageRevision || fileRev != r.FileRevision || hash != r.ContentHash {
		return Fail("CONFLICT", "Source page or attached image changed; ask again using the current image")
	}
	return nil
}
func prepareImageDescription(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, req map[string]any) (any, error) {
	if err := imageOwner(k, e); err != nil {
		return nil, err
	}
	r := ImageDescription{ID: NewID(), PageID: str(req["pageId"]), FileID: str(req["fileId"]), ModelPackID: str(req["modelPackId"]), Question: strings.TrimSpace(str(req["question"])), ActorID: string(e.Actor.ActorId), State: "running", CreatedAt: now(), Warning: "Model interpretation may be incorrect. Verify against the original image; this draft is not indexed or applied to your pages."}
	if r.Question == "" || !utf8.ValidString(r.Question) || utf8.RuneCountInString(r.Question) > 2048 {
		return nil, Fail("INVALID_REQUEST", "Question must contain 1–2048 characters")
	}
	err := k.write(ctx, func(tx *sql.Tx) error {
		if err := k.authorizePage(ctx, tx, e, r.PageID); err != nil {
			return err
		}
		if err := k.authorizeEntity(ctx, tx, e, "file", r.FileID); err != nil {
			return err
		}
		var media string
		var size int64
		err := tx.QueryRowContext(ctx, "SELECT p.revision,f.revision,f.content_hash,f.media_type,f.size_bytes FROM pages p JOIN attachments a ON a.page_id=p.id JOIN files f ON f.id=a.file_id WHERE p.id=? AND f.id=? AND p.deleted_at IS NULL", r.PageID, r.FileID).Scan(&r.PageRevision, &r.FileRevision, &r.ContentHash, &media, &size)
		if err != nil {
			return Fail("NOT_FOUND", "Choose a current image attached to this page")
		}
		if size > 20<<20 || (media != "image/png" && media != "image/jpeg") {
			return Fail("INVALID_REQUEST", "Image analysis accepts PNG/JPEG images up to 20 MiB")
		}
		var running int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM image_descriptions WHERE state='running'").Scan(&running); err != nil {
			return err
		}
		if running > 0 {
			return Fail("RESOURCE_EXHAUSTED", "An image description is already running")
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO image_descriptions(id,page_id,actor_id,state,payload_json,created_at) VALUES(?,?,?,?,?,?)", r.ID, r.PageID, r.ActorID, r.State, string(marshal(r)), r.CreatedAt)
		return err
	})
	return r, err
}
func (k *Kernel) GenerateImageDescription(ctx context.Context, e contracts.ServiceExecutionContext, id string, generate ImageDescriptionGenerator) (ImageDescription, error) {
	r, err := readImageDescription(ctx, k.db, id)
	if err != nil {
		return r, err
	}
	if r.State != "running" {
		return r, Fail("CONFLICT", "Image draft is no longer running")
	}
	if err = k.imageSourceCurrent(ctx, k.db, e, r); err != nil {
		return k.finishImageDescription(context.Background(), e, r, ImageDescriptionResult{}, err)
	}
	f, _, err := k.ReadFile(ctx, e, r.FileID)
	if err != nil {
		return k.finishImageDescription(context.Background(), e, r, ImageDescriptionResult{}, err)
	}
	defer f.Close()
	result, err := generate(ctx, r.ModelPackID, r.Question, f)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return k.finishImageDescription(context.Background(), e, r, result, err)
}
func (k *Kernel) finishImageDescription(ctx context.Context, e contracts.ServiceExecutionContext, r ImageDescription, result ImageDescriptionResult, generationErr error) (ImageDescription, error) {
	err := k.write(ctx, func(tx *sql.Tx) error {
		current, err := readImageDescription(ctx, tx, r.ID)
		if err != nil {
			return err
		}
		if current.ActorID != string(e.Actor.ActorId) {
			return Fail("SCOPE_DENIED", "Image actor changed")
		}
		if current.State != "running" {
			r = current
			return nil
		}
		if err = k.imageSourceCurrent(ctx, tx, e, r); err != nil {
			r.State = "stale"
			r.Stale = true
			r.Error = err.Error()
			r.Text = ""
		} else if generationErr != nil {
			r.State = "failed"
			r.Error = generationErr.Error()
			if errors.Is(generationErr, context.Canceled) || errors.Is(generationErr, context.DeadlineExceeded) {
				r.State = "cancelled"
			}
		} else {
			if result.AnalysisMethod == "pixel_measurement" && result.FinishReason == "measurement" && len(result.Text) <= 2000 && result.Text != "" && result.InputTokens == 0 && result.OutputTokens == 0 && result.ModelHash == "" && result.ProjectorHash == "" && result.HelperHash == "" && result.PreprocessingProfileID == "" && result.PreprocessingProfileHash == "" && result.PeakRSSBytes == 0 && utf8.ValidString(result.Text) {
				r.State = "ready"
				r.Text = result.Text
				r.AnalysisMethod = result.AnalysisMethod
				r.FinishReason = "measurement"
				r.Warning = "Measured from all decoded source pixels before resizing or alpha composition, using encoded orientation. Original bytes are retained; no model interpretation or OCR was performed."
			} else if !validImageProfileProvenance(result) || result.AnalysisMethod != "local_vision_model" || result.Text == "" || len(result.Text) > 16384 || !utf8.ValidString(result.Text) || !imageDigestPattern.MatchString(result.ModelHash) || !imageDigestPattern.MatchString(result.ProjectorHash) || !imageDigestPattern.MatchString(result.HelperHash) || result.InputTokens < 1 || result.InputTokens > 8192 || result.OutputTokens < 0 || result.OutputTokens > 512 || (result.FinishReason != "eos" && result.FinishReason != "length") {
				r.State = "failed"
				r.Error = "Image worker returned an invalid bounded result"
			} else {
				r.State = "ready"
				r.AnalysisMethod = result.AnalysisMethod
				r.Text = result.Text
				r.ModelHash = result.ModelHash
				r.ProjectorHash = result.ProjectorHash
				r.HelperHash = result.HelperHash
				r.PreprocessingProfileID = result.PreprocessingProfileID
				r.PreprocessingProfileHash = result.PreprocessingProfileHash
				r.PeakRSSBytes = result.PeakRSSBytes
				r.InputTokens = result.InputTokens
				r.OutputTokens = result.OutputTokens
				r.FinishReason = result.FinishReason
				if result.FinishReason == "length" {
					r.State = "partial"
					r.Warning = "Output limit reached; this partial description may be incomplete. Verify the original image."
				}
			}
		}
		return storeImageDescription(ctx, tx, r)
	})
	return r, err
}
func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS image_descriptions(id TEXT PRIMARY KEY,page_id TEXT NOT NULL,actor_id TEXT NOT NULL,state TEXT NOT NULL,payload_json TEXT NOT NULL CHECK(json_valid(payload_json)),created_at TEXT NOT NULL); CREATE INDEX IF NOT EXISTS image_descriptions_page ON image_descriptions(page_id,created_at); UPDATE image_descriptions SET state='cancelled',payload_json=json_set(payload_json,'$.state','cancelled','$.error','Application closed during image analysis; ask again to retry.') WHERE state='running'`)
			return err
		})
	})
	RegisterQuery("image.start", prepareImageDescription)
	RegisterQuery("image.list", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, req map[string]any) (any, error) {
		if err := imageOwner(k, e); err != nil {
			return nil, err
		}
		page := str(req["pageId"])
		if err := k.authorizePage(ctx, k.db, e, page); err != nil {
			return nil, err
		}
		rows, err := k.db.QueryContext(ctx, "SELECT id FROM image_descriptions WHERE page_id=? AND actor_id=? ORDER BY created_at DESC,id DESC LIMIT 21", page, e.Actor.ActorId)
		if err != nil {
			return nil, err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		out := []ImageDescription{}
		for _, id := range ids[:min(20, len(ids))] {
			r, err := readImageDescription(ctx, k.db, id)
			if err != nil {
				return nil, err
			}
			if err = k.authorizeEntity(ctx, k.db, e, "file", r.FileID); err != nil {
				continue
			}
			if k.imageSourceCurrent(ctx, k.db, e, r) != nil {
				r.Stale = true
				r.Warning = "Source changed after this draft. Ask again for the current image."
			}
			out = append(out, r)
		}
		images, truncated, err := k.descriptionImages(ctx, e, page)
		if err != nil {
			return nil, err
		}
		return map[string]any{"runs": out, "truncated": len(ids) > 20, "images": images, "imagesTruncated": truncated}, nil
	})
	RegisterQuery("image.cancel", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, req map[string]any) (any, error) {
		if err := imageOwner(k, e); err != nil {
			return nil, err
		}
		var r ImageDescription
		err := k.write(ctx, func(tx *sql.Tx) error {
			var err error
			r, err = readImageDescription(ctx, tx, str(req["runId"]))
			if err != nil {
				return err
			}
			if r.ActorID != string(e.Actor.ActorId) {
				return Fail("SCOPE_DENIED", "Image actor mismatch")
			}
			if err = k.authorizePage(ctx, tx, e, r.PageID); err != nil {
				return err
			}
			if r.State == "running" {
				r.State = "cancelled"
				r.Error = "Cancelled by you."
				return storeImageDescription(ctx, tx, r)
			}
			return nil
		})
		return r, err
	})
}

func (k *Kernel) descriptionImages(ctx context.Context, e contracts.ServiceExecutionContext, page string) ([]contracts.DomainFile, bool, error) {
	rows, err := k.db.QueryContext(ctx, "SELECT f.id FROM files f JOIN attachments a ON a.file_id=f.id WHERE a.page_id=? AND f.media_type IN ('image/png','image/jpeg') ORDER BY f.original_name,f.id LIMIT 101", page)
	if err != nil {
		return nil, false, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, false, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, false, err
	}
	out := []contracts.DomainFile{}
	for _, id := range ids[:min(100, len(ids))] {
		if err = k.authorizeEntity(ctx, k.db, e, "file", id); err != nil {
			return nil, false, err
		}
		f, err := readFileMetadata(ctx, k.db, id)
		if err != nil {
			return nil, false, err
		}
		out = append(out, f)
	}
	return out, len(ids) > 100, nil
}

var imageProfileIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]+$`)

func validImageProfileProvenance(r ImageDescriptionResult) bool {
	if r.PreprocessingProfileID == "" && r.PreprocessingProfileHash == "" {
		return true
	}
	return len(r.PreprocessingProfileID) <= 100 && imageProfileIDPattern.MatchString(r.PreprocessingProfileID) && imageDigestPattern.MatchString(r.PreprocessingProfileHash)
}
