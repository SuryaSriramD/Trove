package workers

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
	"trove.local/phase0/internal/workspace"
)

// ConfigureSearch routes optional semantic work through the same bounded native
// scheduler as chat/transcription. Installing no model never disables FTS.
func ConfigureSearch(k *workspace.Kernel, s *native.Supervisor) {
	if s == nil {
		k.SetEmbeddingProvider(nil)
		return
	}
	k.SetEmbeddingProvider(func(ctx context.Context, model string, texts []string, locations []contracts.DomainLocation) ([][]float32, error) {
		pieces := []string{}
		owners := []int{}
		locs := []contracts.DomainLocation{}
		for i, text := range texts {
			piece := ""
			for _, r := range text {
				if len(piece)+len(string(r)) > 384 {
					pieces = append(pieces, piece)
					owners = append(owners, i)
					locs = append(locs, locations[i])
					piece = ""
				}
				piece += string(r)
			}
			if piece != "" {
				pieces = append(pieces, piece)
				owners = append(owners, i)
				locs = append(locs, locations[i])
			}
		}
		sums := make([][]float32, len(texts))
		for start := 0; start < len(pieces); start += 128 {
			end := start + 128
			if end > len(pieces) {
				end = len(pieces)
			}
			metadata := native.InputMetadata{SchemaVersion: 1, Texts: pieces[start:end], Locations: locs[start:end], ContextTokens: 512}
			f, err := os.CreateTemp("", "trove-embedding-")
			if err != nil {
				return nil, err
			}
			raw, err := func() ([]byte, error) {
				defer os.Remove(f.Name())
				defer f.Close()
				if err := json.NewEncoder(f).Encode(metadata); err != nil {
					return nil, err
				}
				input, err := native.OpenInput(f, "text", workspace.NewID())
				if err != nil {
					return nil, err
				}
				wire := contracts.JobsWorkerRequest{ProtocolVersion: 1, RequestId: contracts.DomainId(workspace.NewID()), JobId: contracts.DomainId(workspace.NewID()), LeaseToken: contracts.DomainId(workspace.NewID()), CancellationId: contracts.DomainId(workspace.NewID()), Operation: "embed", ModelPackId: contracts.DomainId(model), Limits: contracts.JobsResourceLimits{Threads: 2, MemoryBytes: s.MemoryBudget(), TimeoutMs: 300000, MaxOutputBytes: 16 << 20, MaxTokens: 512}}
				priority := native.PriorityBackground
				if len(texts) == 1 {
					priority = native.PriorityChat
				}
				return s.Run(ctx, native.Request{Wire: wire, Inputs: []native.Input{input}, Priority: priority}, nil)
			}()
			if err != nil {
				return nil, err
			}
			var result contracts.JobsWorkerResult
			if err = json.Unmarshal(raw, &result); err != nil {
				return nil, err
			}
			body, ok := result.Value.(contracts.JobsWorkerResultVariant2)
			if !ok || len(body.Vectors) != end-start {
				return nil, workspace.Fail("WORKER_FAILED", "Invalid embedding result")
			}
			for i, vector := range body.Vectors {
				owner := owners[start+i]
				if sums[owner] == nil {
					sums[owner] = make([]float32, len(vector))
				}
				if len(sums[owner]) != len(vector) {
					return nil, workspace.Fail("WORKER_FAILED", "Embedding dimensions changed")
				}
				for n, value := range vector {
					sums[owner][n] += float32(value)
				}
			}
		}
		for _, vector := range sums {
			var norm float64
			for _, v := range vector {
				norm += float64(v) * float64(v)
			}
			if norm == 0 {
				return nil, workspace.Fail("WORKER_FAILED", "Zero embedding vector")
			}
			for i := range vector {
				vector[i] /= float32(math.Sqrt(norm))
			}
		}
		return sums, nil
	})
}
