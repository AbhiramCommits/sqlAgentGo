//go:build ignore

// Command check_coverage asserts that a go coverprofile meets a minimum
// overall statement-coverage threshold.
//
// Usage: go run scripts/check_coverage.go <coverage.out> <min-percent>
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: check_coverage <coverage.out> <min-percent>")
		os.Exit(2)
	}
	path := os.Args[1]
	minPct, err := strconv.ParseFloat(os.Args[2], 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid threshold: %v\n", err)
		os.Exit(2)
	}

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open %s: %v\n", path, err)
		os.Exit(1)
	}
	defer func() { _ = f.Close() }()

	var totalStmt, coveredStmt int64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		// Format: file:startLine.startCol,endLine.endCol numStmt count
		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}
		numStmt, err1 := strconv.ParseInt(parts[1], 10, 64)
		count, err2 := strconv.ParseInt(parts[2], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		totalStmt += numStmt
		if count > 0 {
			coveredStmt += numStmt
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", path, err)
		os.Exit(1)
	}

	if totalStmt == 0 {
		fmt.Fprintln(os.Stderr, "no coverage data found")
		os.Exit(1)
	}
	pct := float64(coveredStmt) / float64(totalStmt) * 100
	fmt.Printf("coverage: %.1f%% (threshold %.1f%%)\n", pct, minPct)
	if pct < minPct {
		fmt.Fprintf(os.Stderr, "coverage %.1f%% is below the %.1f%% threshold\n", pct, minPct)
		os.Exit(1)
	}
}
