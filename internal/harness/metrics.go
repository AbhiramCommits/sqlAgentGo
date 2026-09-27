// Package harness computes evaluation metrics over per-case results and
// renders them as text scoreboards or a markdown report. It is shared by
// `sqlagent eval` and scripts/make_report.go.
package harness

import (
	"fmt"
	"sort"
	"strings"

	"sqlagent/internal/config"
)

// CaseResult is the per-case record written to results.json.
type CaseResult struct {
	CaseID           string   `json:"case_id"`
	SourceDialect    string   `json:"source_dialect"`
	Tags             []string `json:"tags"`
	Difficulty       string   `json:"difficulty"`
	Mode             string   `json:"mode"` // "agent" | "baseline"
	Status           string   `json:"status"`
	Attempts         int      `json:"attempts"`
	WallTimeMs       int64    `json:"wall_time_ms"`
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	EstimatedUSD     float64  `json:"estimated_usd"`
	GuardViolations  int      `json:"guard_violations"`
	FailureClass     string   `json:"failure_class,omitempty"`
	TracePath        string   `json:"trace_path,omitempty"`
	Similarity       float64  `json:"similarity,omitempty"`
	Error            string   `json:"error,omitempty"`
}

// GroupStat aggregates pass-rate metrics for one slice (dialect, tag, ...).
type GroupStat struct {
	Cases        int     `json:"cases"`
	Passed       int     `json:"passed"`
	PassRate     float64 `json:"pass_rate"`
	MeanAttempts float64 `json:"mean_attempts"` // green cases only
}

// Scoreboard is the aggregate over one mode.
type Scoreboard struct {
	Cases                int                  `json:"cases"`
	Passed               int                  `json:"passed"`
	PassRate             float64              `json:"pass_rate"`
	MeanAttempts         float64              `json:"mean_attempts"`
	P90Attempts          float64              `json:"p90_attempts"`
	MeanPromptTokens     float64              `json:"mean_prompt_tokens"`
	MeanCompletionTokens float64              `json:"mean_completion_tokens"`
	MeanUSD              float64              `json:"mean_usd"`
	TotalUSD             float64              `json:"total_usd"`
	GuardCatchRate       float64              `json:"guard_catch_rate"`
	MeanSimilarity       float64              `json:"mean_similarity"`
	FailureClasses       map[string]int       `json:"failure_classes"`
	PerDialect           map[string]GroupStat `json:"per_dialect"`
	PerTag               map[string]GroupStat `json:"per_tag"`
	PerDifficulty        map[string]GroupStat `json:"per_difficulty"`
}

// Report is the full results.json document.
type Report struct {
	GeneratedAt string                 `json:"generated_at"`
	Model       string                 `json:"model"`
	Corpus      string                 `json:"corpus"`
	MaxAttempts int                    `json:"max_attempts"`
	Concurrency int                    `json:"concurrency"`
	Modes       []string               `json:"modes"`
	Cases       []CaseResult           `json:"cases"`
	Scoreboards map[string]*Scoreboard `json:"scoreboards"`
}

// Compute aggregates metrics for one mode's results.
func Compute(results []CaseResult) *Scoreboard {
	sb := &Scoreboard{
		Cases:          len(results),
		FailureClasses: map[string]int{},
		PerDialect:     map[string]GroupStat{},
		PerTag:         map[string]GroupStat{},
		PerDifficulty:  map[string]GroupStat{},
	}

	var attempts []int
	var guardCaught int
	var simSum float64
	var simN int
	var promptSum, completionSum int

	groupAdd := func(m map[string]GroupStat, key string, passed bool, att int) {
		g := m[key]
		g.Cases++
		if passed {
			g.Passed++
			g.MeanAttempts += float64(att)
		}
		m[key] = g
	}

	for _, r := range results {
		passed := r.Status == "green"
		if passed {
			sb.Passed++
			attempts = append(attempts, r.Attempts)
		} else {
			sb.FailureClasses[r.FailureClass]++
		}
		if r.GuardViolations > 0 {
			guardCaught++
		}
		if r.Similarity > 0 {
			simSum += r.Similarity
			simN++
		}
		promptSum += r.PromptTokens
		completionSum += r.CompletionTokens
		sb.TotalUSD += r.EstimatedUSD

		groupAdd(sb.PerDialect, r.SourceDialect, passed, r.Attempts)
		for _, tag := range r.Tags {
			groupAdd(sb.PerTag, tag, passed, r.Attempts)
		}
		groupAdd(sb.PerDifficulty, r.Difficulty, passed, r.Attempts)
	}

	if sb.Cases > 0 {
		sb.PassRate = float64(sb.Passed) / float64(sb.Cases)
		sb.MeanPromptTokens = float64(promptSum) / float64(sb.Cases)
		sb.MeanCompletionTokens = float64(completionSum) / float64(sb.Cases)
		sb.MeanUSD = sb.TotalUSD / float64(sb.Cases)
		sb.GuardCatchRate = float64(guardCaught) / float64(sb.Cases)
	}
	if len(attempts) > 0 {
		sb.MeanAttempts = mean(attempts)
		sb.P90Attempts = p90(attempts)
	}
	if simN > 0 {
		sb.MeanSimilarity = simSum / float64(simN)
	}

	for _, m := range []map[string]GroupStat{sb.PerDialect, sb.PerTag, sb.PerDifficulty} {
		for k, g := range m {
			if g.Cases > 0 {
				g.PassRate = float64(g.Passed) / float64(g.Cases)
			}
			if g.Passed > 0 {
				g.MeanAttempts /= float64(g.Passed)
			}
			m[k] = g
		}
	}
	return sb
}

// Similarity is a token-set Jaccard similarity between two SQL strings,
// used only for reporting against expected_target_sql.
func Similarity(a, b string) float64 {
	ta, tb := tokenize(a), tokenize(b)
	if len(ta) == 0 && len(tb) == 0 {
		return 1
	}
	inter := 0
	for t := range ta {
		if tb[t] {
			inter++
		}
	}
	union := len(ta) + len(tb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func tokenize(s string) map[string]bool {
	out := map[string]bool{}
	for _, t := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_'
	}) {
		out[t] = true
	}
	return out
}

func mean(vals []int) float64 {
	var sum int
	for _, v := range vals {
		sum += v
	}
	return float64(sum) / float64(len(vals))
}

func p90(vals []int) float64 {
	sorted := append([]int(nil), vals...)
	sort.Ints(sorted)
	// Nearest-rank percentile.
	idx := int(float64(len(sorted))*0.9+0.999999) - 1
	if idx < 0 {
		idx = 0
	}
	return float64(sorted[idx])
}

// WorstFailures returns the failed cases sorted worst-first (most attempts,
// then slowest), capped at n.
func WorstFailures(results []CaseResult, n int) []CaseResult {
	var failed []CaseResult
	for _, r := range results {
		if r.Status != "green" {
			failed = append(failed, r)
		}
	}
	sort.Slice(failed, func(i, j int) bool {
		if failed[i].Attempts != failed[j].Attempts {
			return failed[i].Attempts > failed[j].Attempts
		}
		return failed[i].WallTimeMs > failed[j].WallTimeMs
	})
	if len(failed) > n {
		failed = failed[:n]
	}
	return failed
}

// TextScoreboard renders one mode's scoreboard for stdout.
func TextScoreboard(mode string, sb *Scoreboard) string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== %s ===\n", mode)
	fmt.Fprintf(&b, "cases=%d passed=%d pass_rate=%.1f%% mean_attempts=%.2f p90_attempts=%.2f\n",
		sb.Cases, sb.Passed, sb.PassRate*100, sb.MeanAttempts, sb.P90Attempts)
	fmt.Fprintf(&b, "tokens: mean prompt=%.0f completion=%.0f | USD mean=$%.4f total=$%.4f\n",
		sb.MeanPromptTokens, sb.MeanCompletionTokens, sb.MeanUSD, sb.TotalUSD)
	fmt.Fprintf(&b, "guard_catch_rate=%.1f%% mean_similarity=%.3f\n", sb.GuardCatchRate*100, sb.MeanSimilarity)
	fmt.Fprintf(&b, "failures: %s\n", failureHistogram(sb.FailureClasses))

	groups := []struct {
		name string
		m    map[string]GroupStat
	}{{"dialect", sb.PerDialect}, {"difficulty", sb.PerDifficulty}, {"tag", sb.PerTag}}
	for _, g := range groups {
		keys := sortedKeys(g.m)
		for _, k := range keys {
			s := g.m[k]
			fmt.Fprintf(&b, "  %-10s %s: %d/%d (%.1f%%) mean_attempts=%.2f\n",
				g.name, k, s.Passed, s.Cases, s.PassRate*100, s.MeanAttempts)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// TextCompare renders an agent-vs-baseline pass-rate table for stdout.
func TextCompare(agent, baseline *Scoreboard) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-14s %12s %12s\n", "group", "agent pass%", "baseline pass%")
	fmt.Fprintf(&b, "%-14s %11.1f%% %11.1f%%\n", "overall", agent.PassRate*100, baseline.PassRate*100)
	for _, k := range sortedKeys(agent.PerDialect) {
		fmt.Fprintf(&b, "%-14s %11.1f%% %11.1f%%\n",
			"dialect:"+k, agent.PerDialect[k].PassRate*100, baseline.PerDialect[k].PassRate*100)
	}
	for _, k := range sortedKeys(agent.PerDifficulty) {
		fmt.Fprintf(&b, "%-14s %11.1f%% %11.1f%%\n",
			"diff:"+k, agent.PerDifficulty[k].PassRate*100, baseline.PerDifficulty[k].PassRate*100)
	}
	for _, k := range sortedKeys(agent.PerTag) {
		fmt.Fprintf(&b, "%-14s %11.1f%% %11.1f%%\n",
			"tag:"+k, agent.PerTag[k].PassRate*100, baseline.PerTag[k].PassRate*100)
	}
	fmt.Fprintf(&b, "\nattempts (green): agent mean=%.2f p90=%.2f | baseline mean=%.2f\n",
		agent.MeanAttempts, agent.P90Attempts, baseline.MeanAttempts)
	fmt.Fprintf(&b, "cost: agent total=$%.4f | baseline total=$%.4f\n", agent.TotalUSD, baseline.TotalUSD)
	fmt.Fprintf(&b, "guard catch rate: %.1f%%\n", agent.GuardCatchRate*100)
	return b.String()
}

func failureHistogram(m map[string]int) string {
	keys := sortedKeys(m)
	if len(keys) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, " ")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// CostUSD is the harness-level cost helper (kept here so make_report and
// eval share it, independent of config).
func CostUSD(promptTokens, completionTokens int, p config.ModelPrice) float64 {
	return p.CostUSD(promptTokens, completionTokens)
}
