# SQL dialect conversion — evaluation report

- generated: 2026-09-27T09:28:34Z
- corpus: `testdata/corpus` (35 cases)
- model: `llama3.2:3b`
- max_attempts: 4, concurrency: 4
- modes: agent, baseline

## Headline: agentic loop vs naive prompting

| group | agent pass% | baseline pass% | agent Δ |
| --- | --- | --- | --- |
| overall | 62.9% (22/35) | 40.0% (14/35) | +22.9 pts |
| dialect: oracle | 35.3% (6/17) | 29.4% (5/17) | +5.9 pts |
| dialect: tsql | 88.9% (16/18) | 50.0% (9/18) | +38.9 pts |
| difficulty: easy | 45.5% (5/11) | 36.4% (4/11) | +9.1 pts |
| difficulty: hard | 72.7% (8/11) | 45.5% (5/11) | +27.3 pts |
| difficulty: medium | 69.2% (9/13) | 38.5% (5/13) | +30.8 pts |
| tag: cte | 100.0% (3/3) | 33.3% (1/3) | +66.7 pts |
| tag: date_fn | 42.9% (3/7) | 14.3% (1/7) | +28.6 pts |
| tag: implicit_cast | 100.0% (3/3) | 33.3% (1/3) | +66.7 pts |
| tag: join | 75.0% (6/8) | 50.0% (4/8) | +25.0 pts |
| tag: null_fn | 25.0% (1/4) | 0.0% (0/4) | +25.0 pts |
| tag: rownum | 33.3% (1/3) | 100.0% (3/3) | -66.7 pts |
| tag: string_fn | 50.0% (2/4) | 25.0% (1/4) | +25.0 pts |
| tag: top_n | 66.7% (2/3) | 100.0% (3/3) | -33.3 pts |
| tag: window | 83.3% (5/6) | 66.7% (4/6) | +16.7 pts |

## agent mode

| metric | value |
| --- | --- |
| pass rate | 62.9% (22/35) |
| mean attempts (green) | 1.55 |
| p90 attempts (green) | 2.00 |
| mean prompt tokens | 8886 |
| mean completion tokens | 525 |
| mean USD | $0.0000 |
| total USD | $0.0000 |
| guard catch rate | 5.7% |
| mean similarity to reference | 0.741 |

### failure classes

| class | count |
| --- | --- |
| exec_error | 13 |

## baseline mode

| metric | value |
| --- | --- |
| pass rate | 40.0% (14/35) |
| mean attempts (green) | 1.00 |
| p90 attempts (green) | 1.00 |
| mean prompt tokens | 124 |
| mean completion tokens | 59 |
| mean USD | $0.0000 |
| total USD | $0.0000 |
| guard catch rate | 0.0% |
| mean similarity to reference | 0.831 |

### failure classes

| class | count |
| --- | --- |
| exec_error | 20 |
| result_mismatch | 1 |

## worst failures

| case | mode | dialect | difficulty | failure class | attempts | wall ms | trace |
| --- | --- | --- | --- | --- | --- | --- | --- |
| orcl_months_between | agent | oracle | hard | exec_error | 4 | 502245 | `traces/orcl_months_between/20260927T085821.771579000.jsonl` |
| orcl_nulls_last | agent | oracle | medium | exec_error | 4 | 452169 | `traces/orcl_nulls_last/20260927T085140.619914000.jsonl` |
| orcl_nvl | agent | oracle | easy | exec_error | 4 | 434789 | `traces/orcl_nvl/20260927T085033.012257000.jsonl` |
| orcl_string_concat | agent | oracle | easy | exec_error | 4 | 382349 | `traces/orcl_string_concat/20260927T085847.729919000.jsonl` |
| tsql_null_comparison | agent | tsql | medium | exec_error | 4 | 370736 | `traces/tsql_null_comparison/20260927T091459.991615000.jsonl` |
