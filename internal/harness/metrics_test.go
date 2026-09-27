package harness

import (
	"testing"
)

func green(id, dialect, tag, diff string, attempts, guardViol int, prompt, completion int) CaseResult {
	return CaseResult{
		CaseID:           id,
		SourceDialect:    dialect,
		Tags:             []string{tag},
		Difficulty:       diff,
		Mode:             "agent",
		Status:           "green",
		Attempts:         attempts,
		PromptTokens:     prompt,
		CompletionTokens: completion,
		GuardViolations:  guardViol,
		EstimatedUSD:     0.01,
	}
}

func failed(id, dialect, tag, diff, class string, attempts, guardViol int) CaseResult {
	r := green(id, dialect, tag, diff, attempts, guardViol, 100, 50)
	r.Status = "exhausted"
	r.FailureClass = class
	return r
}

func TestComputeScoreboard(t *testing.T) {
	results := []CaseResult{
		green("a", "tsql", "top_n", "easy", 1, 0, 100, 50),
		green("b", "tsql", "top_n", "easy", 3, 1, 300, 150),
		green("c", "oracle", "rownum", "hard", 2, 0, 200, 100),
		green("d", "oracle", "rownum", "hard", 4, 2, 400, 200),
		failed("e", "tsql", "join", "medium", "guard_violation", 4, 3),
		failed("f", "oracle", "date_fn", "hard", "result_mismatch", 2, 0),
	}
	sb := Compute(results)

	if sb.Cases != 6 || sb.Passed != 4 {
		t.Fatalf("cases/passed = %d/%d", sb.Cases, sb.Passed)
	}
	if want := 4.0 / 6.0; sb.PassRate != want {
		t.Fatalf("pass rate = %v, want %v", sb.PassRate, want)
	}
	// Green attempts: 1, 2, 3, 4 -> mean 2.5, p90 = 4.
	if sb.MeanAttempts != 2.5 {
		t.Fatalf("mean attempts = %v, want 2.5", sb.MeanAttempts)
	}
	if sb.P90Attempts != 4 {
		t.Fatalf("p90 attempts = %v, want 4", sb.P90Attempts)
	}
	// Guard caught on 3 of 6 runs (b, d, e).
	if want := 3.0 / 6.0; sb.GuardCatchRate != want {
		t.Fatalf("guard catch rate = %v, want %v", sb.GuardCatchRate, want)
	}
	if sb.FailureClasses["guard_violation"] != 1 || sb.FailureClasses["result_mismatch"] != 1 {
		t.Fatalf("failure classes = %v", sb.FailureClasses)
	}
	if sb.PerDialect["tsql"].Passed != 2 || sb.PerDialect["oracle"].Passed != 2 {
		t.Fatalf("per dialect = %+v", sb.PerDialect)
	}
	if sb.PerTag["top_n"].Cases != 2 || sb.PerTag["rownum"].Cases != 2 {
		t.Fatalf("per tag = %+v", sb.PerTag)
	}
	if sb.PerDifficulty["hard"].Cases != 3 {
		t.Fatalf("per difficulty = %+v", sb.PerDifficulty)
	}
	if sb.TotalUSD == 0 || sb.MeanPromptTokens == 0 {
		t.Fatalf("token/usd aggregates missing: %+v", sb)
	}
}

func TestWorstFailures(t *testing.T) {
	results := []CaseResult{
		failed("e1", "tsql", "join", "easy", "guard_violation", 4, 0),
		failed("e2", "tsql", "join", "easy", "exec_error", 2, 0),
		failed("e3", "tsql", "join", "easy", "result_mismatch", 3, 0),
		green("g", "tsql", "join", "easy", 1, 0, 100, 50),
	}
	worst := WorstFailures(results, 2)
	if len(worst) != 2 {
		t.Fatalf("worst = %d, want 2", len(worst))
	}
	if worst[0].CaseID != "e1" || worst[1].CaseID != "e3" {
		t.Fatalf("order = %s, %s", worst[0].CaseID, worst[1].CaseID)
	}
}

func TestSimilarity(t *testing.T) {
	if s := Similarity("SELECT a FROM t", "SELECT a FROM t"); s != 1 {
		t.Fatalf("identical similarity = %v", s)
	}
	if s := Similarity("alpha beta", "gamma delta"); s != 0 {
		t.Fatalf("disjoint similarity = %v", s)
	}
	if s := Similarity("select A from T order by x", "SELECT a FROM t"); s <= 0 || s >= 1 {
		t.Fatalf("partial similarity = %v", s)
	}
}

func TestMarkdownRenders(t *testing.T) {
	results := []CaseResult{
		green("a", "tsql", "top_n", "easy", 1, 0, 100, 50),
		failed("b", "oracle", "rownum", "hard", "guard_violation", 4, 2),
	}
	agent := Compute(results)
	report := &Report{
		GeneratedAt: "2026-09-27",
		Model:       "test",
		Corpus:      "testdata/corpus",
		Modes:       []string{"agent"},
		Cases:       results,
		Scoreboards: map[string]*Scoreboard{"agent": agent},
	}
	md := Markdown(report)
	for _, want := range []string{"evaluation report", "pass rate", "guard_violation", "worst failures", "b |"} {
		if !contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
