package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed dialects.yaml
var dialectsYAML []byte

type dialectEntry struct {
	Name      string   `yaml:"name"`
	Keywords  []string `yaml:"keywords"`
	Dialects  []string `yaml:"dialects"`
	Snowflake string   `yaml:"snowflake"`
	Notes     string   `yaml:"notes"`
}

// DialectRef answers questions about how source-dialect constructs translate
// to Snowflake, backed by the curated dialects.yaml mapping.
type DialectRef struct {
	entries []dialectEntry
}

// NewDialectRef loads the curated construct mappings from the embedded
// dialects.yaml.
func NewDialectRef() (*DialectRef, error) {
	var doc struct {
		Constructs []dialectEntry `yaml:"constructs"`
	}
	if err := yaml.Unmarshal(dialectsYAML, &doc); err != nil {
		return nil, fmt.Errorf("parse dialects.yaml: %w", err)
	}
	if len(doc.Constructs) == 0 {
		return nil, fmt.Errorf("dialects.yaml contains no constructs")
	}
	return &DialectRef{entries: doc.Constructs}, nil
}

func (d *DialectRef) Name() string { return "dialect_ref" }

func (d *DialectRef) Description() string {
	return "Look up how a specific SQL construct in a source dialect (tsql or oracle) translates to Snowflake."
}

func (d *DialectRef) JSONSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["construct", "source_dialect"],
  "properties": {
    "construct": {
      "type": "string",
      "description": "The construct to look up, e.g. TOP, NVL, ROWNUM, DATEADD, string concatenation"
    },
    "source_dialect": {
      "type": "string",
      "enum": ["tsql", "oracle"],
      "description": "The source dialect the construct comes from"
    }
  }
}`)
}

func (d *DialectRef) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	var req struct {
		Construct     string `json:"construct"`
		SourceDialect string `json:"source_dialect"`
	}
	if err := unmarshalArgs(args, &req); err != nil {
		return "", err
	}
	req.Construct = strings.ToLower(strings.TrimSpace(req.Construct))
	req.SourceDialect = strings.ToLower(strings.TrimSpace(req.SourceDialect))
	if req.Construct == "" {
		return "", fmt.Errorf("construct is required")
	}
	switch req.SourceDialect {
	case "tsql", "oracle":
	default:
		return "", fmt.Errorf("unsupported source_dialect %q (want tsql or oracle)", req.SourceDialect)
	}

	var matches []dialectEntry
	for _, e := range d.entries {
		if !dialectListContains(e.Dialects, req.SourceDialect) {
			continue
		}
		if constructMatches(e, req.Construct) {
			matches = append(matches, e)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf(
			"no mapping for construct %q in dialect %q; available constructs: %s",
			req.Construct, req.SourceDialect, d.availableFor(req.SourceDialect))
	}

	var b strings.Builder
	for _, e := range matches {
		fmt.Fprintf(&b, "%s (source: %s)\n", e.Name, req.SourceDialect)
		fmt.Fprintf(&b, "  snowflake: %s\n", e.Snowflake)
		notes := strings.TrimSpace(e.Notes)
		if notes != "" {
			for _, line := range strings.Split(notes, "\n") {
				fmt.Fprintf(&b, "  notes: %s\n", strings.TrimSpace(line))
			}
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (d *DialectRef) availableFor(dialect string) string {
	var names []string
	for _, e := range d.entries {
		if dialectListContains(e.Dialects, dialect) {
			names = append(names, e.Name)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func dialectListContains(list []string, want string) bool {
	for _, d := range list {
		if strings.EqualFold(d, want) {
			return true
		}
	}
	return false
}

func constructMatches(e dialectEntry, construct string) bool {
	if strings.Contains(strings.ToLower(e.Name), construct) {
		return true
	}
	for _, k := range e.Keywords {
		if strings.Contains(construct, strings.ToLower(k)) || strings.Contains(strings.ToLower(k), construct) {
			return true
		}
	}
	return false
}
