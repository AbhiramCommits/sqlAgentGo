# Results

## Current run

One real measurement: 35 corpus cases × {agentic loop, naive baseline} against
`llama3.2:3b` via local Ollama, 4 max attempts, concurrency 4. Full per-case
data in `results.json`; every run is reproducible with
`go run ./cmd/sqlagent eval --corpus testdata/corpus --compare`.

### Scoreboard (agent vs baseline)

| group | agent pass% | baseline pass% | Δ |
| --- | --- | --- | --- |
| overall | 62.9% (22/35) | 40.0% (14/35) | +22.9 pts |
| oracle | 35.3% (6/17) | 29.4% (5/17) | +5.9 pts |
| tsql | 88.9% (16/18) | 50.0% (9/18) | +38.9 pts |
| easy | 45.5% (5/11) | 36.4% (4/11) | +9.1 pts |
| medium | 69.2% (9/13) | 38.5% (5/13) | +30.8 pts |
| hard | 72.7% (8/11) | 45.5% (5/11) | +27.3 pts |

### Cost and latency per conversion

| metric | agent | baseline |
| --- | --- | --- |
| pass rate | 62.9% | 40.0% |
| mean attempts to green | 1.55 (p90 2.0) | 1.00 |
| mean wall time | 226s (p90 371s) | 33s (p90 48s) |
| mean prompt tokens | 8,885 | 124 |
| mean completion tokens | 524 | 58 |
| mean USD | $0.0000 (local model) | $0.0000 |
| total run USD | $0.0000 | $0.0000 |
| guard catch rate | 5.7% of runs | — (no guard) |
| failure classes | 13 exec_error | 20 exec_error, 1 result_mismatch |

Notes:

- The 3B local model is slow (Ollama on CPU): the agentic loop's 226s mean is
  dominated by per-call model latency, not tool or engine work. On a hosted
  model with ~1s completions the same loop should run 10-30s per case.
- USD is $0 because `llama3.2:3b` is priced at $0 in `configs/config.yaml`;
  swap in a hosted model and the price table takes over automatically.
- The agent's edge over baseline is concentrated where repair and tools
  matter (medium/hard cases, CTEs, implicit casts); baseline wins on the
  trivial `rownum`/`top_n` cases where one shot suffices.

## What I'd do next

1. **AST-level cost model for candidate ranking.** Right now the loop commits
   to one candidate per attempt and only the differential oracle judges it.
   Score each generated candidate *before* execution — estimated join/cardinality
   blowups from the parse tree, dialect-construct risk from `dialect_ref`
   coverage, edit distance from the source — and execute the cheapest
   plausible candidate first. Turns repair from serial retries into a
   ranked search.
2. **Few-shot retrieval from past green conversions.** Every green run is a
   verified (source, target) pair sitting in `traces/`. Index them by tag and
   AST shape (the corpus tags + a hash of the select structure), and retrieve
   the 2-3 nearest verified examples into the act prompt. Verified examples
   are worth more than hand-written ones because they carry oracle-approved
   translations for exactly the constructs this corpus exercises.
3. **Parallel candidate sampling with diff-based voting.** Ask the model for
   N candidate translations (or run N attempts concurrently), execute each,
   and let the oracle vote: candidates producing identical result sets form
   the consensus answer; split results trigger targeted repair. With an
   LLM-backed executor this is cheap to parallelize and it converts
   non-deterministic sampling from a liability into an uncertainty estimate.
