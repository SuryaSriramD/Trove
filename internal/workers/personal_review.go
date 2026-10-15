package workers

import (
	"context"
	"time"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
	"trove.local/phase0/internal/workspace"
)

type personalReviewGenerator interface {
	GenerationContext(string) (int, error)
	CountTokens(context.Context, string, []string) ([]int, error)
	GenerateTextWithGrammar(context.Context, string, string, int, string, func(string)) (string, error)
}

// RunPersonalReviews drains explicitly activated review occurrences on a host.
// It uses only configured installed packs; interactive model/provider controls
// remain desktop-only. Native supervisor scheduling bounds shared model work.
func RunPersonalReviews(ctx context.Context, k *workspace.Kernel, s *native.Supervisor) error {
	var generator personalReviewGenerator
	if s != nil {
		generator = s
	}
	return runPersonalReviews(ctx, k, generator, 2*time.Second)
}
func runPersonalReviews(ctx context.Context, k *workspace.Kernel, generator personalReviewGenerator, poll time.Duration) error {
	owner := k.OwnerContext()
	defer k.RevokeGrant(string(owner.ScopeGrantId))
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		run, err := k.NextPersonalReviewGeneration(ctx, owner)
		if err != nil {
			return err
		}
		if run.ID != "" {
			runPersonalReview(ctx, k, owner, run, generator)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func runPersonalReview(ctx context.Context, k *workspace.Kernel, owner contracts.ServiceExecutionContext, run workspace.PersonalReviewRun, generator personalReviewGenerator) {
	fail := func(err error) {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = k.FailPersonalReviewGeneration(cleanup, owner, run.ID, err)
	}
	if run.Generative == nil {
		fail(workspace.Fail("INVALID_REQUEST", "Queued review is missing its model configuration; prepare a fresh review."))
		return
	}
	if generator == nil {
		fail(workspace.Fail("WORKER_FAILED", "The host has no instruction model configured. Install the selected pack, restart with --models and --native-bundle, then retry this review."))
		return
	}
	size, err := generator.GenerationContext(run.Generative.ModelPackID)
	if err != nil {
		fail(err)
		return
	}
	generation, cancel := context.WithTimeout(ctx, 5*time.Minute)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-generation.Done():
				return
			case <-ticker.C:
				if k.CheckPersonalReviewGeneration(generation, owner, run.ID) != nil {
					cancel()
					return
				}
			}
		}
	}()
	_, _ = k.GeneratePersonalReview(generation, owner, run.ID, workspace.AIBudget{ContextTokens: size, OutputTokens: min(2048, size/4), CountTokens: func(ctx context.Context, prompts []workspace.AIPrompt) ([]int, error) {
		texts := make([]string, len(prompts))
		for i, p := range prompts {
			texts[i] = native.ChatPrompt(p.System, p.User)
		}
		return generator.CountTokens(ctx, run.Generative.ModelPackID, texts)
	}}, func(ctx context.Context, system, prompt string, tokens int, grammar string, stream func(string)) (string, error) {
		return generator.GenerateTextWithGrammar(ctx, run.Generative.ModelPackID, native.ChatPrompt(system, prompt), tokens, grammar, stream)
	})
	cancel()
	<-done
}
