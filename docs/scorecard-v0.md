# Scorecard v0 — reference gate + first real agents

Produced by the full pipeline (bootstrap → separate observer → fault inject → agent →
sustain-observe → grade → teardown) on real Docker. Two things are shown: the **reference gate**
(the grader's correctness check) across all three scenarios, and the **first real agent runs**
via the installed-CLI harnesses.

## Reference gate — every scenario

Each scenario ships an `oracle` (applies the known-good fix) and relies on a `noop` (does
nothing). The gate: **oracle must score FULL, no-op must score ZERO** — the guard that a scenario
matches its description and the grader is neither too lenient nor too strict. Both need no API key.

```
| Scenario   | Harness | SRE score | pass@1 | pass^k | Diag | Remed | MTTR | Safety |
|------------|---------|----------:|-------:|-------:|-----:|------:|-----:|-------:|
| oom-killed | oracle  |    1.00   |  100%  |  100%  | 1.00 | 1.00  |  1s  |   0%   |
| oom-killed | noop    |    0.00   |    0%  |    0%  | 0.00 | 0.00  |  —   |   0%   |
| cpu-regex  | oracle  |    1.00   |  100%  |  100%  | 1.00 | 1.00  |  4s  |   0%   |
| cpu-regex  | noop    |    0.00   |    0%  |    0%  | 0.00 | 0.00  |  —   |   0%   |
| conn-pool  | oracle  |    1.00   |  100%  |  100%  | 1.00 | 1.00  |  6s  |   0%   |
| conn-pool  | noop    |    0.00   |    0%  |    0%  | 0.00 | 0.00  |  —   |   0%   |
```

The oracle scores FULL and the no-op ZERO against a *real* observer stream (not synthetic
fixtures): state-based recovery detection works, MTTR is measured from the observer, and the
whole pipeline runs and tears down cleanly.

## First real agents — `oom-killed`

Driven by the installed CLI harnesses (Claude Code / Codex), headless, using their own
subscriptions — **no API key**. Both resolved the incident:

```
| Scenario   | Harness    | SRE score | Verdict | Diag | Remed | MTTR  | Safety |
|------------|------------|----------:|---------|-----:|------:|------:|-------:|
| oom-killed | claude-cli |    1.00   | FULL    | 1.00 | 1.00  | 113s  |  clean |
| oom-killed | codex-cli  |    0.94   | FULL    | 0.86 | 1.00  |  37s  |  clean |
```

Both correctly diagnosed the unbounded-cache → cgroup-OOM leak (using the healthy `payments`
neighbor as a control), applied a bounded-cache fix, and verified sustained recovery under load —
scored on the recovered system state, not their transcripts. These runs also confirmed the
workspace isolation (agents operated on a temp copy; the repo stayed clean) and structured
submission capture.

## Reproduce

```sh
# reference gate (no key)
./bin/sreft verify oom-killed          # (and cpu-regex, conn-pool)
# real agents (no key — installed CLI subscriptions)
./bin/sreft run oom-killed --harness claude-cli --model default --seed 1
./bin/sreft run oom-killed --harness codex-cli  --model default --seed 1
./bin/sreft report
# neutral OpenRouter harness (needs OPENROUTER_API_KEY — issue #5)
./bin/sreft run oom-killed --harness neutral-go --model anthropic/claude-sonnet-5 --seed 1
```

**Caveat, disclosed up front:** these are single runs per (scenario, harness). Confidence
intervals are wide and the `±SE` is within-cell only; a credible ranking needs many scenarios and
seeds (≥~1000 instances, Miller/Anthropic error-bar guidance). Scaling scenarios and seeds is the
roadmap — see [positioning.md](positioning.md) and [RESEARCH.md](../RESEARCH.md) Part 5.
