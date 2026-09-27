package corpus

import (
	"path/filepath"
	"testing"
)

func TestLoadDirCorpus(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "corpus")
	c, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}

	if len(c.Cases) < 30 {
		t.Fatalf("expected at least 30 cases, got %d", len(c.Cases))
	}

	dialects := map[string]int{}
	difficulties := map[string]int{}
	tags := map[string]int{}
	expected := 0
	for _, tc := range c.Cases {
		dialects[tc.SourceDialect]++
		difficulties[tc.Difficulty]++
		for _, tag := range tc.Tags {
			tags[tag]++
		}
		if tc.ExpectedTargetSQL != "" {
			expected++
		}
	}

	if dialects["tsql"] == 0 || dialects["oracle"] == 0 {
		t.Fatalf("both dialects required, got %v", dialects)
	}
	for _, d := range []string{"easy", "medium", "hard"} {
		if difficulties[d] == 0 {
			t.Fatalf("difficulty %q has no cases: %v", d, difficulties)
		}
	}
	for tag := range ValidTags {
		if tags[tag] == 0 {
			t.Fatalf("tag %q has no cases: %v", tag, tags)
		}
	}
	if expected == 0 {
		t.Fatal("expected_target_sql must be present on at least one case")
	}
}

func TestValidateRejectsBadCases(t *testing.T) {
	cases := []Corpus{
		{Cases: []Case{}},
		{Cases: []Case{{ID: "x"}}},
		{Cases: []Case{{ID: "x", SourceDialect: "mysql", SourceSQL: "SELECT 1", Tags: []string{"join"}, Difficulty: "easy"}}},
		{Cases: []Case{{ID: "x", SourceDialect: "tsql", SourceSQL: "SELECT 1", Tags: []string{"bogus"}, Difficulty: "easy"}}},
		{Cases: []Case{{ID: "x", SourceDialect: "tsql", SourceSQL: "SELECT 1", Tags: []string{"join"}, Difficulty: "impossible"}}},
		{Cases: []Case{{ID: "x", SourceDialect: "tsql", SourceSQL: "", Tags: []string{"join"}, Difficulty: "easy"}}},
		{Cases: []Case{
			{ID: "x", SourceDialect: "tsql", SourceSQL: "SELECT 1", Tags: []string{"join"}, Difficulty: "easy"},
			{ID: "x", SourceDialect: "tsql", SourceSQL: "SELECT 2", Tags: []string{"join"}, Difficulty: "easy"},
		}},
	}
	for i, c := range cases {
		if err := c.Validate(); err == nil {
			t.Fatalf("case #%d: expected validation error", i+1)
		}
	}
}
