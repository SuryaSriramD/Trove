// phase3-benchmark measures public service paths in disposable workspaces.
// It is a developer tool, never an end-user runtime dependency.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
	"trove.local/phase0/internal/workers"
	"trove.local/phase0/internal/workspace"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func request(k *workspace.Kernel, e contracts.ServiceExecutionContext, method string, fields map[string]any) map[string]any {
	r := map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": k.ID()}
	for key, v := range fields {
		r[key] = v
	}
	raw, err := json.Marshal(r)
	must(err)
	var out map[string]any
	must(json.Unmarshal(k.Dispatch(context.Background(), e, method, raw), &out))
	if out["ok"] != true {
		panic(fmt.Sprintf("%s: %s", method, rawJSON(out)))
	}
	return out["result"].(map[string]any)
}
func rawJSON(v any) []byte { b, err := json.Marshal(v); must(err); return b }
func batch(k *workspace.Kernel, e contracts.ServiceExecutionContext, commands, guards []any) {
	request(k, e, "batch", map[string]any{"mutation": map[string]any{"scopeGrantId": e.ScopeGrantId, "idempotencyKey": workspace.NewID(), "expectedRevisions": guards}, "commands": commands})
}
func guard(kind, id, revision string) any {
	return map[string]any{"target": map[string]any{"entityType": kind, "entityId": id}, "revision": revision}
}
func stats(samples []float64) map[string]any {
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	sum := 0.0
	for _, v := range samples {
		sum += v
	}
	quantile := func(q float64) float64 { return sorted[int(math.Ceil(q*float64(len(sorted))))-1] }
	return map[string]any{"samplesMs": samples, "count": len(samples), "meanMs": sum / float64(len(samples)), "p50Ms": quantile(.5), "p95Ms": quantile(.95), "maxMs": sorted[len(sorted)-1]}
}
func size(path string) int64 {
	var total int64
	must(filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	}))
	return total
}
func measure(k *workspace.Kernel, e contracts.ServiceExecutionContext, mode string, n int) map[string]any {
	queries := []string{"release", "telescope", "café", "project07", "absenttoken"}
	run := func(i int) {
		out := request(k, e, "search.query", map[string]any{"query": queries[i%len(queries)], "mode": mode, "limit": 20})
		if out["mode"] != mode || out["partial"] != false {
			panic(fmt.Sprintf("search degraded: %s", rawJSON(out)))
		}
		if i%len(queries) == 4 && len(out["hits"].([]any)) != 0 && mode == "keyword" {
			panic("unexpected keyword hit")
		}
		if i%len(queries) != 4 && len(out["hits"].([]any)) == 0 {
			panic("expected search hits")
		}
	}
	for i := 0; i < 5; i++ {
		run(i)
	}
	samples := []float64{}
	for i := 0; i < n; i++ {
		start := time.Now()
		run(i)
		samples = append(samples, float64(time.Since(start))/float64(time.Millisecond))
	}
	out := stats(samples)
	out["queries"] = queries
	out["warmupQueries"] = 5
	out["scope"] = "public Go Dispatch, validation + retrieval + response serialization; excludes Wails bridge and paint"
	return out
}

// Measure separately from retrieval to expose the cost of a fresh process and
// model on every request. The load milestone includes validation, hashing and
// process launch; it is not a claim to isolate only model deserialization.
func embeddingBreakdown(s *native.Supervisor, page string) map[string]any {
	f, err := os.CreateTemp("", "trove-benchmark-query-")
	must(err)
	defer os.Remove(f.Name())
	defer f.Close()
	must(json.NewEncoder(f).Encode(native.InputMetadata{SchemaVersion: 1, ContextTokens: 512, Texts: []string{"telescope"}, Locations: []contracts.DomainLocation{{PageId: contracts.DomainId(page), SourceRevision: "21"}}}))
	input, err := native.OpenInput(f, "text", workspace.NewID())
	must(err)
	loadTimes, computeTimes, totalTimes, loadFractions := []float64{}, []float64{}, []float64{}, []float64{}
	for range 10 {
		wire := contracts.JobsWorkerRequest{ProtocolVersion: 1, RequestId: contracts.DomainId(workspace.NewID()), JobId: contracts.DomainId(workspace.NewID()), LeaseToken: contracts.DomainId(workspace.NewID()), CancellationId: contracts.DomainId(workspace.NewID()), Operation: "embed", ModelPackId: "01990000-0000-7000-8000-000000000102", Limits: contracts.JobsResourceLimits{Threads: 2, MemoryBytes: s.MemoryBudget(), TimeoutMs: 300000, MaxOutputBytes: 16 << 20, MaxTokens: 512}}
		start := time.Now()
		loaded, computed := 0.0, 0.0
		_, err := s.Run(context.Background(), native.Request{Wire: wire, Inputs: []native.Input{input}, Priority: native.PriorityChat}, func(event native.Event) {
			if event.Kind != "progress" {
				return
			}
			var progress struct {
				Message string `json:"message"`
			}
			must(json.Unmarshal(event.Progress, &progress))
			elapsed := float64(time.Since(start)) / float64(time.Millisecond)
			if progress.Message == "Model loaded" {
				loaded = elapsed
			}
			if progress.Message == "Embedding computed" {
				computed = elapsed
			}
		})
		must(err)
		total := float64(time.Since(start)) / float64(time.Millisecond)
		if loaded == 0 || computed < loaded {
			panic("missing embedding timing milestones")
		}
		loadTimes = append(loadTimes, loaded)
		computeTimes = append(computeTimes, computed-loaded)
		totalTimes = append(totalTimes, total)
		loadFractions = append(loadFractions, loaded/total)
	}
	out := map[string]any{"toModelLoaded": stats(loadTimes), "afterLoadToEmbeddingComputed": stats(computeTimes), "total": stats(totalTimes), "preLoadFractions": loadFractions, "scope": "10 separate native requests, one short query each, two CPU threads, no GPU offload; OS caches are warm. Pre-load includes admission/model hash verification, process startup, and loading."}
	return out
}

func runLeased(k *workspace.Kernel, job contracts.JobsJob, handler func(context.Context, contracts.JobsJob) error) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- handler(ctx, job) }()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-ticker.C:
			if err := k.HeartbeatJob(ctx, string(job.JobId), string(*job.LeaseToken)); err != nil {
				cancel()
				<-done
				return err
			}
		}
	}
}
func profile(base string, pages, iterations int, modelRoot, bundle string, hybrid bool) map[string]any {
	ctx := context.Background()
	dir := filepath.Join(base, fmt.Sprintf("pages-%d", pages))
	start := time.Now()
	k, err := workspace.Create(dir)
	must(err)
	defer func() { must(k.Close()) }()
	e := k.OwnerContext()
	first := ""
	textBytes := 0
	for n := 0; n < pages; n++ {
		page := workspace.NewID()
		if first == "" {
			first = page
		}
		title := fmt.Sprintf("project%02d Release research page %05d", n%25, n)
		commands := []any{map[string]any{"kind": "page.create", "pageId": page, "title": title}}
		guards := []any{guard("page", page, "0")}
		textBytes += len(title)
		for b := 0; b < 2; b++ {
			block := workspace.NewID()
			text := fmt.Sprintf("Project%02d telescope observations from café review. Release notes %05d section %d. Durable local research, offline evidence and product planning. ", n%25, n, b)
			text = strings.Repeat(text, 3)
			textBytes += len(text)
			commands = append(commands, map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": text}}}}})
			guards = append(guards, guard("block", block, "0"))
		}
		batch(k, e, commands, guards)
	}
	setup := time.Since(start).Seconds()
	start = time.Now()
	must(k.FlushSearch(ctx))
	indexMs := float64(time.Since(start)) / float64(time.Millisecond)
	out := map[string]any{"pages": pages, "paragraphBlocks": pages * 2, "canonicalTextUTF8Bytes": textBytes, "setupSeconds": setup, "initialIndexMs": indexMs, "workspacePath": dir, "workspaceBytesBeforeSemantic": size(dir), "keyword": measure(k, e, "keyword", iterations), "keywordModelsInstalled": false}
	edits := []float64{}
	for i := 0; i < 20; i++ {
		start = time.Now()
		batch(k, e, []any{map[string]any{"kind": "page.update", "pageId": first, "title": fmt.Sprintf("project00 Release research edited %d", i)}}, []any{guard("page", first, strconv.Itoa(i+1))})
		edits = append(edits, float64(time.Since(start))/float64(time.Millisecond))
	}
	out["idleEditCommit"] = stats(edits)
	must(k.FlushSearch(ctx))
	must(k.Close())
	opens := []float64{}
	for i := 0; i < 5; i++ {
		start = time.Now()
		k, err = workspace.Open(dir)
		must(err)
		opens = append(opens, float64(time.Since(start))/float64(time.Millisecond))
		if i < 4 {
			must(k.Close())
		}
	}
	e = k.OwnerContext()
	out["warmKernelOpen"] = stats(opens)
	if hybrid {
		manager, err := native.NewManager(modelRoot)
		must(err)
		supervisor, err := native.NewSupervisor(bundle, manager)
		must(err)
		workers.ConfigureSearch(k, supervisor)
		request(k, e, "search.configureSemantic", map[string]any{"enabled": true, "modelPackId": "01990000-0000-7000-8000-000000000102", "dimension": 384})
		start = time.Now()
		handlers := k.RegisteredJobHandlers()
		jobs := 0
		for {
			must(k.QueueSemanticUpdates(ctx, e))
			j, err := k.ClaimJob(ctx, []string{"GENERATE_EMBEDDINGS"})
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					break
				}
				panic(err)
			}
			if j.JobId == "" {
				break
			}
			must(runLeased(k, j, handlers["GENERATE_EMBEDDINGS"]))
			must(k.FinishJob(ctx, string(j.JobId), string(*j.LeaseToken), nil, false))
			jobs++
		}
		out["semanticIndexSeconds"] = time.Since(start).Seconds()
		out["semanticJobs"] = jobs
		out["embeddingPackId"] = "01990000-0000-7000-8000-000000000102"
		out["hybrid"] = measure(k, e, "hybrid", 10)
		out["embeddingWorkerBreakdown"] = embeddingBreakdown(supervisor, first)
	}
	out["workspaceBytesFinal"] = size(dir)
	return out
}
func main() {
	pages := flag.String("pages", "100,1000,10000", "Comma-separated deterministic workspace profiles")
	iterations := flag.Int("iterations", 100, "Measured keyword queries per profile")
	root := flag.String("workspaces", "", "Disposable workspace parent (new directory)")
	models := flag.String("models", "/private/tmp/trove-phase1-models", "Installed model cache")
	bundle := flag.String("bundle", "build/native", "Native bundle")
	hybrid := flag.Bool("hybrid", false, "Measure real native hybrid retrieval on the first profile")
	output := flag.String("output", "evidence/phase3/benchmark-service.json", "JSON evidence output")
	flag.Parse()
	var err error
	*bundle, err = filepath.Abs(*bundle)
	must(err)
	*models, err = filepath.Abs(*models)
	must(err)
	if *iterations < 5 {
		panic("at least five iterations required")
	}
	if *root == "" {
		var err error
		*root, err = os.MkdirTemp("", "trove-phase3-benchmark-")
		must(err)
	} else {
		must(os.Mkdir(*root, 0700))
	}
	results := []any{}
	for i, s := range strings.Split(*pages, ",") {
		n, err := strconv.Atoi(s)
		must(err)
		if n < 1 {
			panic("page count must be positive")
		}
		fmt.Fprintf(os.Stderr, "Benchmarking %d pages\n", n)
		results = append(results, profile(*root, n, *iterations, *models, *bundle, *hybrid && i == 0))
	}
	out := map[string]any{"schemaVersion": 1, "recordedAt": time.Now().UTC().Format(time.RFC3339Nano), "fixtureVersion": "p0-pages-v1", "profiles": results, "limitations": []string{"Service timings exclude Wails bridge, browser paint and interaction latency.", "No physical disk cache flush: kernel reopen is warm, not cold application launch.", "Benchmark does not establish recording acoustics or model answer quality.", "Other qualification jobs may run on the same host; no thermal or power isolation is claimed."}}
	must(os.MkdirAll(filepath.Dir(*output), 0755))
	raw, err := json.MarshalIndent(out, "", "  ")
	must(err)
	must(os.WriteFile(*output, append(raw, '\n'), 0644))
	fmt.Println(*output)
}
