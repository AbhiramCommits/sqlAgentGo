// Command make_report renders the markdown evaluation report from a
// results.json produced by `sqlagent eval`.
//
// Usage: go run scripts/make_report.go [results.json] [-out report.md]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"sqlagent/internal/harness"
)

func main() {
	var outPath string
	flag.StringVar(&outPath, "out", "report.md", "output markdown file")
	flag.Parse()

	inPath := "results.json"
	if flag.NArg() > 0 {
		inPath = flag.Arg(0)
	}

	data, err := os.ReadFile(inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", inPath, err)
		os.Exit(1)
	}
	var report harness.Report
	if err := json.Unmarshal(data, &report); err != nil {
		fmt.Fprintf(os.Stderr, "parse %s: %v\n", inPath, err)
		os.Exit(1)
	}

	md := harness.Markdown(&report)
	if err := os.WriteFile(outPath, []byte(md), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", outPath, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s from %s (%d result rows)\n", outPath, inPath, len(report.Cases))
}
