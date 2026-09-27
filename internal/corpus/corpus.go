// Package corpus loads dialect-conversion test cases from YAML files.
//
// A case's source_sql must execute on the source engine (Postgres) because
// the differential oracle runs it there; it is written in the closest
// Postgres-valid form of the source dialect's intent. expected_target_sql is
// Snowflake-flavored reference SQL used only for similarity reporting, never
// for pass/fail: the differential execution oracle is the sole judge.
package corpus

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Corpus is a collection of conversion cases.
type Corpus struct {
	Cases []Case `yaml:"cases"`
}

// Case is a single source query to translate.
type Case struct {
	ID            string   `yaml:"id"`
	SourceDialect string   `yaml:"source_dialect"` // "tsql" or "oracle"
	SourceSQL     string   `yaml:"source_sql"`
	Tags          []string `yaml:"tags"`
	Difficulty    string   `yaml:"difficulty"` // easy | medium | hard
	// ExpectedTargetSQL is Snowflake-flavored reference SQL, used only to
	// report similarity; it never affects pass/fail.
	ExpectedTargetSQL string `yaml:"expected_target_sql,omitempty"`
}

// ValidTags is the tag vocabulary.
var ValidTags = map[string]bool{
	"top_n": true, "date_fn": true, "null_fn": true, "window": true,
	"cte": true, "join": true, "string_fn": true, "rownum": true,
	"implicit_cast": true,
}

// Load reads a single YAML corpus file.
func Load(path string) (*Corpus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read corpus %s: %w", path, err)
	}
	var c Corpus
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse corpus %s: %w", path, err)
	}
	return &c, nil
}

// LoadDir reads every *.yaml file in dir (non-recursive) and merges cases,
// sorted by file name for determinism.
func LoadDir(dir string) (*Corpus, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read corpus dir %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ext := filepath.Ext(e.Name()); ext == ".yaml" || ext == ".yml" {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)

	merged := &Corpus{}
	for _, f := range files {
		c, err := Load(f)
		if err != nil {
			return nil, err
		}
		merged.Cases = append(merged.Cases, c.Cases...)
	}
	return merged, nil
}

// Validate checks required fields, id uniqueness, dialect, difficulty, and
// tag vocabulary. It does not execute anything.
func (c *Corpus) Validate() error {
	if len(c.Cases) == 0 {
		return fmt.Errorf("corpus is empty")
	}
	seen := map[string]bool{}
	for i, tc := range c.Cases {
		where := fmt.Sprintf("case #%d", i+1)
		if tc.ID == "" {
			return fmt.Errorf("%s: missing id", where)
		}
		if seen[tc.ID] {
			return fmt.Errorf("%s: duplicate id %q", where, tc.ID)
		}
		seen[tc.ID] = true
		switch tc.SourceDialect {
		case "tsql", "oracle":
		default:
			return fmt.Errorf("case %q: invalid source_dialect %q", tc.ID, tc.SourceDialect)
		}
		if strings.TrimSpace(tc.SourceSQL) == "" {
			return fmt.Errorf("case %q: missing source_sql", tc.ID)
		}
		switch tc.Difficulty {
		case "easy", "medium", "hard":
		default:
			return fmt.Errorf("case %q: invalid difficulty %q", tc.ID, tc.Difficulty)
		}
		if len(tc.Tags) == 0 {
			return fmt.Errorf("case %q: missing tags", tc.ID)
		}
		for _, t := range tc.Tags {
			if !ValidTags[t] {
				return fmt.Errorf("case %q: unknown tag %q", tc.ID, t)
			}
		}
	}
	return nil
}
