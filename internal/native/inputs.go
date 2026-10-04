package native

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"trove.local/phase0/contracts"
)

type SegmentIdentity struct {
	SegmentID string `json:"segmentId"`
	BlockID   string `json:"blockId"`
}

// InputMetadata is the versioned private descriptor format, separate from the
// shared WorkerRequest envelope. SchemaVersion must be one; unknown keys fail.
type InputMetadata struct {
	SchemaVersion int                        `json:"schemaVersion"`
	Text          string                     `json:"text,omitempty"`
	Grammar       string                     `json:"grammar,omitempty"`
	Texts         []string                   `json:"texts,omitempty"`
	Locations     []contracts.DomainLocation `json:"locations,omitempty"`
	ContextTokens int                        `json:"contextTokens,omitempty"`
	GPULayers     int                        `json:"gpuLayers,omitempty"`
	MeetingID     string                     `json:"meetingId,omitempty"`
	SegmentIDs    []SegmentIdentity          `json:"segmentIds,omitempty"`
	Language      string                     `json:"language,omitempty"`
	UseGPU        bool                       `json:"useGPU,omitempty"`
}

func validateInputMetadata(p Pack, operation string, inputs []Input) (InputMetadata, error) {
	var m InputMetadata
	if len(inputs) < 1 || inputs[0].Handle.Kind != "text" || inputs[0].Handle.SizeBytes > 4<<20 {
		return m, errors.New("INVALID_INPUT: missing metadata")
	}
	raw := make([]byte, inputs[0].Handle.SizeBytes)
	if _, e := io.ReadFull(io.NewSectionReader(inputs[0].File, 0, inputs[0].Handle.SizeBytes), raw); e != nil {
		return m, e
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&m); e != nil {
		return m, e
	}
	if m.SchemaVersion != 1 {
		return m, errors.New("INVALID_INPUT: metadata version")
	}
	if operation == "generate" || operation == "embed" || operation == "tokenize" {
		if len(inputs) != 1 || m.ContextTokens < 32 || m.ContextTokens > p.ContextTokens || m.ContextTokens > 32768 || (m.GPULayers != 0 && m.GPULayers != -1) || m.MeetingID != "" || len(m.SegmentIDs) > 0 || m.Language != "" || m.UseGPU {
			return m, errors.New("INVALID_INPUT: model context or inputs")
		}
	}
	switch operation {
	case "tokenize":
		if m.Text != "" || len(m.Texts) == 0 || len(m.Texts) > 16 || len(m.Locations) != 0 || m.Grammar != "" || m.GPULayers != 0 {
			return m, errors.New("INVALID_INPUT: tokenizer batch")
		}
		total := 0
		for _, text := range m.Texts {
			total += len(text)
			if text == "" || len(text) > 128<<10 || total > 1<<20 {
				return m, errors.New("INVALID_INPUT: tokenizer text budget")
			}
		}
	case "generate":
		if m.Text == "" || len(m.Text) > 1<<20 || len(m.Texts) > 0 || len(m.Locations) > 0 || len(m.Grammar) > 64<<10 {
			return m, errors.New("INVALID_INPUT: prompt or grammar")
		}
	case "embed":
		if m.Text != "" || len(m.Texts) == 0 || len(m.Texts) > 256 || len(m.Texts) != len(m.Locations) || m.Grammar != "" {
			return m, errors.New("INVALID_INPUT: embedding batch")
		}
		for i, text := range m.Texts {
			if text == "" {
				return m, errors.New("INVALID_INPUT: empty embedding text")
			}
			raw, _ := json.Marshal(m.Locations[i])
			if e := contracts.Validate("domain", "Location", raw); e != nil {
				return m, e
			}
		}
	case "transcribe":
		if len(inputs) != 2 || inputs[1].Handle.Kind != "audio" || inputs[1].Handle.SizeBytes%4 != 0 || inputs[1].Handle.SizeBytes > 460800000 || !uuidPattern.MatchString(m.MeetingID) || len(m.SegmentIDs) == 0 || len(m.SegmentIDs) > 256 || !regexp.MustCompile(`^[a-z]{2,3}$`).MatchString(m.Language) || m.Text != "" || len(m.Texts) > 0 || len(m.Locations) > 0 || m.ContextTokens != 0 || m.GPULayers != 0 || m.Grammar != "" {
			return m, errors.New("INVALID_INPUT: transcript metadata")
		}
		ids := map[string]bool{}
		for _, id := range m.SegmentIDs {
			if !uuidPattern.MatchString(id.SegmentID) || !uuidPattern.MatchString(id.BlockID) || ids[id.SegmentID] || ids[id.BlockID] {
				return m, errors.New("INVALID_INPUT: reserved segment identities")
			}
			ids[id.SegmentID] = true
			ids[id.BlockID] = true
		}
	default:
		return m, errors.New("INVALID_INPUT: operation")
	}
	return m, nil
}
