# Scorecard v0 — reference validation

This is the first end-to-end scorecard, produced by the full pipeline (bootstrap → separate
observer process → fault inject → agent → sustain-observe → grade → teardown) on real Docker.

It uses the two **reference harnesses** rather than live models, because they are the grader's
correctness gate and need no API key:

- `oracle` — applies the scenario's known-good fix (`CACHE_MAX`) and submits a correct RCA.
- `noop` — does nothing; the incident is left failing.

```
| Scenario   | Model  | Harness | N | SRE score (±SE) | pass@1 | pass^k | Diag | Remed | MTTR (med) | Safety viol. |
|------------|--------|---------|--:|----------------:|-------:|-------:|-----:|------:|-----------:|-------------:|
| oom-killed | oracle | oracle  | 1 |     1.00 ±0.00  |  100%  |  100%  | 1.00 | 1.00  |     1s     |      0%      |
| oom-killed | noop   | noop    | 1 |     0.00 ±0.00  |    0%  |    0%  | 0.00 | 0.00  |     —      |      0%      |
```

**What this validates:** the oracle scores **FULL** and the no-op scores **ZERO** against a
*real* observer stream (not synthetic fixtures) — the grader is neither too lenient nor too
strict, the state-based recovery detection works, MTTR is measured from the observer, and the
whole pipeline runs and tears down cleanly. This is the guard the whole benchmark rests on.

## What's next (needs `OPENROUTER_API_KEY` — issue #5)

Real model rows come from the neutral OpenRouter harness:

```sh
./bin/sreft run oom-killed --model anthropic/claude-sonnet-5 --seed 1
./bin/sreft run oom-killed --model openai/gpt-5            --seed 1
./bin/sreft run oom-killed --model google/gemini-2.5-pro  --seed 1
# ... ≥3 seeds each ...
./bin/sreft report --out docs/scorecard.md
```

**Caveat, disclosed up front:** v0 is a single scenario. Confidence intervals are wide and the
`±SE` shown is within-model only; a credible cross-model ranking needs many scenarios and
≥~1000 instances (Miller/Anthropic error-bar guidance). Scaling scenarios is the roadmap — see
[positioning.md](positioning.md) and [RESEARCH.md](../RESEARCH.md) Part 5.
