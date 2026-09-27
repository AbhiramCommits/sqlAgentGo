package harness

import (
	"fmt"
	"strings"
)

// Markdown renders the full markdown report from a results.json Report.
func Markdown(r *Report) string {
	var b strings.Builder

	b.WriteString("# SQL dialect conversion — evaluation report\n\n")
	fmt.Fprintf(&b, "- generated: %s\n", r.GeneratedAt)
	fmt.Fprintf(&b, "- corpus: `%s` (%d cases)\n", r.Corpus, len(r.Cases)/max(1, len(r.Modes)))
	fmt.Fprintf(&b, "- model: `%s`\n", r.Model)
	fmt.Fprintf(&b, "- max_attempts: %d, concurrency: %d\n", r.MaxAttempts, r.Concurrency)
	fmt.Fprintf(&b, "- modes: %s\n\n", strings.Join(r.Modes, ", "))

	if len(r.Scoreboards) == 0 {
		b.WriteString("_no results_\n")
		return b.String()
	}

	agent := r.Scoreboards["agent"]
	baseline := r.Scoreboards["baseline"]

	if agent != nil && baseline != nil {
		b.WriteString("## Headline: agentic loop vs naive prompting\n\n")
		b.WriteString("| group | agent pass% | baseline pass% | agent Δ |\n")
		b.WriteString("| --- | --- | --- | --- |\n")
		fmt.Fprintf(&b, "| overall | %.1f%% (%d/%d) | %.1f%% (%d/%d) | %+.1f pts |\n",
			agent.PassRate*100, agent.Passed, agent.Cases,
			baseline.PassRate*100, baseline.Passed, baseline.Cases,
			(agent.PassRate-baseline.PassRate)*100)
		for _, k := range sortedKeys(agent.PerDialect) {
			a, bb := agent.PerDialect[k], baseline.PerDialect[k]
			fmt.Fprintf(&b, "| dialect: %s | %.1f%% (%d/%d) | %.1f%% (%d/%d) | %+.1f pts |\n",
				k, a.PassRate*100, a.Passed, a.Cases, bb.PassRate*100, bb.Passed, bb.Cases,
				(a.PassRate-bb.PassRate)*100)
		}
		for _, k := range sortedKeys(agent.PerDifficulty) {
			a, bb := agent.PerDifficulty[k], baseline.PerDifficulty[k]
			fmt.Fprintf(&b, "| difficulty: %s | %.1f%% (%d/%d) | %.1f%% (%d/%d) | %+.1f pts |\n",
				k, a.PassRate*100, a.Passed, a.Cases, bb.PassRate*100, bb.Passed, bb.Cases,
				(a.PassRate-bb.PassRate)*100)
		}
		for _, k := range sortedKeys(agent.PerTag) {
			a, bb := agent.PerTag[k], baseline.PerTag[k]
			fmt.Fprintf(&b, "| tag: %s | %.1f%% (%d/%d) | %.1f%% (%d/%d) | %+.1f pts |\n",
				k, a.PassRate*100, a.Passed, a.Cases, bb.PassRate*100, bb.Passed, bb.Cases,
				(a.PassRate-bb.PassRate)*100)
		}
		b.WriteString("\n")
	}

	for _, mode := range r.Modes {
		sb := r.Scoreboards[mode]
		if sb == nil {
			continue
		}
		fmt.Fprintf(&b, "## %s mode\n\n", mode)
		fmt.Fprintf(&b, "| metric | value |\n| --- | --- |\n")
		fmt.Fprintf(&b, "| pass rate | %.1f%% (%d/%d) |\n", sb.PassRate*100, sb.Passed, sb.Cases)
		fmt.Fprintf(&b, "| mean attempts (green) | %.2f |\n", sb.MeanAttempts)
		fmt.Fprintf(&b, "| p90 attempts (green) | %.2f |\n", sb.P90Attempts)
		fmt.Fprintf(&b, "| mean prompt tokens | %.0f |\n", sb.MeanPromptTokens)
		fmt.Fprintf(&b, "| mean completion tokens | %.0f |\n", sb.MeanCompletionTokens)
		fmt.Fprintf(&b, "| mean USD | $%.4f |\n", sb.MeanUSD)
		fmt.Fprintf(&b, "| total USD | $%.4f |\n", sb.TotalUSD)
		fmt.Fprintf(&b, "| guard catch rate | %.1f%% |\n", sb.GuardCatchRate*100)
		fmt.Fprintf(&b, "| mean similarity to reference | %.3f |\n", sb.MeanSimilarity)
		b.WriteString("\n")

		b.WriteString("### failure classes\n\n")
		if len(sb.FailureClasses) == 0 {
			b.WriteString("_none_\n\n")
		} else {
			b.WriteString("| class | count |\n| --- | --- |\n")
			for _, k := range sortedKeys(sb.FailureClasses) {
				fmt.Fprintf(&b, "| %s | %d |\n", k, sb.FailureClasses[k])
			}
			b.WriteString("\n")
		}
	}

	// Worst failures across all modes, with trace paths.
	b.WriteString("## worst failures\n\n")
	failed := WorstFailures(r.Cases, 5)
	if len(failed) == 0 {
		b.WriteString("_no failures_\n")
	} else {
		b.WriteString("| case | mode | dialect | difficulty | failure class | attempts | wall ms | trace |\n")
		b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
		for _, f := range failed {
			trace := f.TracePath
			if trace == "" {
				trace = "-"
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %d | %d | `%s` |\n",
				f.CaseID, f.Mode, f.SourceDialect, f.Difficulty, f.FailureClass,
				f.Attempts, f.WallTimeMs, trace)
		}
	}
	return b.String()
}
