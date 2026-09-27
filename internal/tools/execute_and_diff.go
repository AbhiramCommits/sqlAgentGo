package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"sqlagent/internal/exec"
)

// ExecuteAndDiff runs the source query on the source engine and the target
// (Snowflake-flavored) query on the target engine, returning a compact diff
// report. This is the differential-verification primitive.
type ExecuteAndDiff struct {
	Source exec.Executor
	Target exec.Executor
}

func (d *ExecuteAndDiff) Name() string { return "execute_and_diff" }

func (d *ExecuteAndDiff) Description() string {
	return "Run source SQL on the source engine and target SQL on the target engine, then diff the two result sets."
}

func (d *ExecuteAndDiff) JSONSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["source_sql", "target_sql"],
  "properties": {
    "source_sql": {
      "type": "string",
      "description": "The original source-dialect query"
    },
    "target_sql": {
      "type": "string",
      "description": "The translated Snowflake query to verify"
    }
  }
}`)
}

func (d *ExecuteAndDiff) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	if d.Source == nil || d.Target == nil {
		return "", fmt.Errorf("execute_and_diff requires both source and target executors")
	}
	var req struct {
		SourceSQL string `json:"source_sql"`
		TargetSQL string `json:"target_sql"`
	}
	if err := unmarshalArgs(args, &req); err != nil {
		return "", err
	}
	if req.SourceSQL == "" || req.TargetSQL == "" {
		return "", fmt.Errorf("both source_sql and target_sql are required")
	}

	srcRes, err := d.Source.Run(ctx, req.SourceSQL)
	if err != nil {
		return "", fmt.Errorf("source engine error: %w", err)
	}
	tgtRes, err := d.Target.Run(ctx, req.TargetSQL)
	if err != nil {
		return "", fmt.Errorf("target engine error: %w", err)
	}

	report := exec.Diff(srcRes, tgtRes)
	return report.String(), nil
}
