package exec

import (
	"fmt"
	"strings"
)

// SplitStatements splits a SQL script into individual statements. It is a
// minimal splitter: it honors single-quoted strings ('...' with ” escapes)
// and -- line comments, and splits on top-level semicolons. It is sufficient
// for the controlled db/*.sql seed scripts.
func SplitStatements(script string) ([]string, error) {
	var stmts []string
	var cur strings.Builder
	inString := false
	lineComment := false

	flush := func() {
		s := strings.TrimSpace(cur.String())
		cur.Reset()
		if s != "" {
			stmts = append(stmts, s)
		}
	}

	runes := []rune(script)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		if lineComment {
			if c == '\n' {
				lineComment = false
				cur.WriteRune(c)
			}
			continue
		}
		if inString {
			cur.WriteRune(c)
			if c == '\'' {
				if i+1 < len(runes) && runes[i+1] == '\'' {
					cur.WriteRune(runes[i+1])
					i++
					continue
				}
				inString = false
			}
			continue
		}
		switch {
		case c == '\'':
			inString = true
			cur.WriteRune(c)
		case c == '-' && i+1 < len(runes) && runes[i+1] == '-':
			lineComment = true
			i++
		case c == ';':
			flush()
		default:
			cur.WriteRune(c)
		}
	}
	if inString {
		return nil, fmt.Errorf("unterminated string literal in SQL script")
	}
	flush()
	return stmts, nil
}
