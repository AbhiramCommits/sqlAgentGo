package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"sqlagent/internal/agent"
	"sqlagent/internal/config"
	"sqlagent/internal/corpus"
	"sqlagent/internal/harness"
	"sqlagent/internal/schema"
)

const (
	modeAgent    = "agent"
	modeBaseline = "baseline"
)

func newEvalCmd() *cobra.Command {
	var (
		corpusDir   string
		concurrency int
		maxAttempts int
		model       string
		out         string
		baseline    bool
		compare     bool
	)
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Evaluate the conversion agent against a corpus",
		Long: `Runs every corpus case through the agent (and optionally the naive
baseline), judging pass/fail only by differential execution, and writes a
scoreboard to stdout and full results to results.json.

--compare runs both the agentic loop and the baseline on the identical
corpus and reports them side by side.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			c, err := corpus.LoadDir(corpusDir)
			if err != nil {
				return err
			}
			if err := c.Validate(); err != nil {
				return err
			}

			cfg, err := config.Load("configs", ".")
			if err != nil {
				return err
			}
			modelName := model
			if modelName == "" {
				modelName = cfg.LLM.Model
			}

			sch, pg, dk, reg, err := buildPipeline(ctx)
			if err != nil {
				return err
			}

			// The differential oracle executes every source_sql on Postgres;
			// fail loudly if the corpus itself is broken.
			for _, tc := range c.Cases {
				if _, err := pg.Run(ctx, tc.SourceSQL); err != nil {
					return fmt.Errorf("corpus case %q fails on the source engine: %w", tc.ID, err)
				}
			}

			a := agent.New(buildLLMClient(cfg, modelName), reg,
				agent.WithExecutors(pg, dk),
				agent.WithTraceDir("traces"),
				agent.WithMaxAttempts(maxAttempts),
			)

			modes := []string{modeAgent}
			if baseline || compare {
				modes = []string{modeBaseline}
			}
			if compare {
				modes = []string{modeAgent, modeBaseline}
			}

			// The effective attempt budget: 0 means the agent default of 4.
			effectiveMaxAttempts := maxAttempts
			if effectiveMaxAttempts <= 0 {
				effectiveMaxAttempts = 4
			}

			price := cfg.PriceFor(modelName)
			results := runCorpus(ctx, a, c.Cases, modes, sch, price, concurrency)

			scoreboards := map[string]*harness.Scoreboard{}
			for _, mode := range modes {
				scoreboards[mode] = harness.Compute(filterByMode(results, mode))
			}

			report := &harness.Report{
				GeneratedAt: time.Now().UTC().Format(time.RFC3339),
				Model:       modelName,
				Corpus:      corpusDir,
				MaxAttempts: effectiveMaxAttempts,
				Concurrency: concurrency,
				Modes:       modes,
				Cases:       results,
				Scoreboards: scoreboards,
			}

			data, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(out, data, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", out, err)
			}

			if compare {
				fmt.Fprintln(cmd.OutOrStdout(), harness.TextCompare(scoreboards[modeAgent], scoreboards[modeBaseline]))
			} else {
				for _, mode := range modes {
					fmt.Fprintln(cmd.OutOrStdout(), harness.TextScoreboard(mode, scoreboards[mode]))
				}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s (%d cases, %s)\n", out, len(c.Cases), modesString(modes))
			return nil
		},
	}
	cmd.Flags().StringVar(&corpusDir, "corpus", "testdata/corpus", "corpus directory")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "max concurrent case runs")
	cmd.Flags().IntVar(&maxAttempts, "max-attempts", 0, "max agent attempts per case (default 4)")
	cmd.Flags().StringVar(&model, "model", "", "override the configured model")
	cmd.Flags().StringVar(&out, "out", "results.json", "results file")
	cmd.Flags().BoolVar(&baseline, "baseline", false, "run only the naive single-call baseline")
	cmd.Flags().BoolVar(&compare, "compare", false, "run agent and baseline side by side")
	return cmd
}

func modesString(modes []string) string {
	s := ""
	for i, m := range modes {
		if i > 0 {
			s += "+"
		}
		s += m
	}
	return s
}

// runCorpus runs every case in every mode with errgroup-bounded concurrency,
// preserving corpus order in the results.
func runCorpus(ctx context.Context, a *agent.Agent, cases []corpus.Case, modes []string, sch *schema.Schema, price config.ModelPrice, concurrency int) []harness.CaseResult {
	type key struct {
		caseIdx int
		mode    string
	}
	type slot struct {
		k key
		r harness.CaseResult
	}

	n := len(cases) * len(modes)
	slots := make(chan slot, n)
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for i, tc := range cases {
		for _, mode := range modes {
			i, tc, mode := i, tc, mode
			g.Go(func() error {
				slots <- slot{k: key{i, mode}, r: runOneCase(ctx, a, tc, mode, sch, price)}
				return nil
			})
		}
	}
	if err := g.Wait(); err != nil {
		// runOneCase never returns an error; this is a safety net.
		panic(err)
	}
	close(slots)

	results := make([]harness.CaseResult, n)
	for s := range slots {
		results[s.k.caseIdx*len(modes)+modeIndex(modes, s.k.mode)] = s.r
	}
	return results
}

func modeIndex(modes []string, mode string) int {
	for i, m := range modes {
		if m == mode {
			return i
		}
	}
	return 0
}

// runOneCase executes one case in one mode and records the measurement.
func runOneCase(ctx context.Context, a *agent.Agent, tc corpus.Case, mode string, sch *schema.Schema, price config.ModelPrice) harness.CaseResult {
	st := &agent.State{
		CaseID:        tc.ID,
		SourceSQL:     tc.SourceSQL,
		SourceDialect: tc.SourceDialect,
		Schema:        *sch,
	}
	start := time.Now()
	var res *agent.RunResult
	var runErr error
	if mode == modeBaseline {
		res, runErr = a.RunBaseline(ctx, st)
	} else {
		res, runErr = a.Run(ctx, st)
	}
	wall := time.Since(start).Milliseconds()

	var promptTokens, completionTokens, violations int
	for _, s := range st.Trace {
		promptTokens += s.PromptTokens
		completionTokens += s.CompletionTokens
		violations += s.Violations
	}

	r := harness.CaseResult{
		CaseID:           tc.ID,
		SourceDialect:    tc.SourceDialect,
		Tags:             tc.Tags,
		Difficulty:       tc.Difficulty,
		Mode:             mode,
		Status:           st.Status,
		Attempts:         st.Attempt,
		WallTimeMs:       wall,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		EstimatedUSD:     price.CostUSD(promptTokens, completionTokens),
		GuardViolations:  violations,
		FailureClass:     st.FailureClass,
	}
	if runErr != nil {
		r.Status = "llm_error"
		r.FailureClass = agent.FailureLLMError
		r.Error = runErr.Error()
	}
	if res != nil {
		r.TracePath = res.TracePath
	}
	if tc.ExpectedTargetSQL != "" && st.Candidate != "" {
		r.Similarity = harness.Similarity(st.Candidate, tc.ExpectedTargetSQL)
	}
	return r
}

func filterByMode(results []harness.CaseResult, mode string) []harness.CaseResult {
	var out []harness.CaseResult
	for _, r := range results {
		if r.Mode == mode {
			out = append(out, r)
		}
	}
	return out
}
