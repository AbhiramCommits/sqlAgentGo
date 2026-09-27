package harness

import (
	"strings"
	"testing"
)

func TestTextScoreboardRenders(t *testing.T) {
	results := []CaseResult{
		green("a", "tsql", "top_n", "easy", 1, 0, 100, 50),
		green("b", "oracle", "rownum", "hard", 2, 1, 200, 100),
		failed("c", "tsql", "join", "medium", "guard_violation", 4, 3),
	}
	sb := Compute(results)
	out := TextScoreboard("agent", sb)
	for _, want := range []string{
		"=== agent ===", "pass_rate=66.7%", "mean_attempts=1.50",
		"guard_catch_rate=66.7%", "guard_violation=1", "dialect", "difficulty", "tag",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("scoreboard missing %q:\n%s", want, out)
		}
	}
}

func TestTextCompareRenders(t *testing.T) {
	agent := Compute([]CaseResult{green("a", "tsql", "top_n", "easy", 1, 0, 10, 5)})
	baseline := Compute([]CaseResult{failed("a", "tsql", "top_n", "easy", "exec_error", 1, 0)})
	out := TextCompare(agent, baseline)
	for _, want := range []string{"overall", "100.0%", "0.0%", "guard catch rate"} {
		if !strings.Contains(out, want) {
			t.Fatalf("compare missing %q:\n%s", want, out)
		}
	}
}

func TestFailureHistogramEmpty(t *testing.T) {
	if got := failureHistogram(map[string]int{}); got != "none" {
		t.Fatalf("empty histogram = %q", got)
	}
}

func TestCostUSD(t *testing.T) {
	got := CostUSD(1000000, 0, ModelPriceForTest())
	if got != 2.5 {
		t.Fatalf("CostUSD = %v", got)
	}
}

func TestSortedKeys(t *testing.T) {
	keys := sortedKeys(map[string]int{"b": 1, "a": 2, "c": 3})
	if strings.Join(keys, "") != "abc" {
		t.Fatalf("sortedKeys = %v", keys)
	}
}

func TestMarkdownWithoutResults(t *testing.T) {
	md := Markdown(&Report{})
	if !strings.Contains(md, "_no results_") {
		t.Fatalf("empty report: %s", md)
	}
}

func TestMarkdownCompareSection(t *testing.T) {
	agent := Compute([]CaseResult{green("a", "tsql", "top_n", "easy", 1, 0, 10, 5)})
	baseline := Compute([]CaseResult{failed("a", "tsql", "top_n", "easy", "exec_error", 1, 0)})
	r := &Report{
		GeneratedAt: "2026-09-27",
		Model:       "test",
		Corpus:      "testdata/corpus",
		Modes:       []string{"agent", "baseline"},
		Cases:       []CaseResult{green("a", "tsql", "top_n", "easy", 1, 0, 10, 5), failed("a", "tsql", "top_n", "easy", "exec_error", 1, 0)},
		Scoreboards: map[string]*Scoreboard{"agent": agent, "baseline": baseline},
	}
	md := Markdown(r)
	for _, want := range []string{"Headline", "agent Δ", "worst failures", "exec_error", "trace"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}
