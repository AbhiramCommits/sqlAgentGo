package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// tracePath returns traces/<case-id>/<timestamp>.jsonl for a run starting at
// t. The nanosecond timestamp keeps repeated runs of the same case distinct.
func tracePath(dir, caseID string, t time.Time) string {
	name := t.UTC().Format("20060102T150405.000000000") + ".jsonl"
	return filepath.Join(dir, caseID, name)
}

// writeTrace persists every step as one JSON line. Traces are the resume
// artifact for a run: each line carries the node, latency, token counts,
// tool calls with arguments, and the candidate SQL at that point, so the
// file is complete and greppable.
func writeTrace(path string, steps []Step) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create trace dir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create trace file: %w", err)
	}
	defer f.Close()

	bw := bufio.NewWriter(f)
	enc := json.NewEncoder(bw)
	for _, s := range steps {
		if err := enc.Encode(s); err != nil {
			return fmt.Errorf("encode trace step: %w", err)
		}
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("flush trace: %w", err)
	}
	return f.Sync()
}
