// Package corpus loads dialect-conversion test cases from YAML files.
package corpus

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// Corpus is a collection of conversion cases.
type Corpus struct {
	Cases []Case `yaml:"cases"`
}

// Case is a single source query to translate, with an optional expected
// Snowflake translation used by future automated evaluation.
type Case struct {
	Name     string `yaml:"name"`
	Dialect  string `yaml:"dialect"` // "tsql" or "oracle"
	Query    string `yaml:"query"`
	Expected string `yaml:"expected,omitempty"`
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
