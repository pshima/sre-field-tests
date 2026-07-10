# Running benchmarks

A single run is `sreft run` (one instance). A **benchmark** is `sreft bench` — a whole matrix of
scenarios × harness/model cells × seeds, run reproducibly into a self-contained directory.

## Suites (the definition)

A **suite** is a committed file that declares the matrix, so a benchmark is reproducible from
source the same way a scenario is. See [`suites/`](../suites).

```yaml
# suites/cli-sweep.yaml
name: cli-sweep
tier: tier0-docker
seeds: 3
scenarios: [oom-killed, cpu-regex, conn-pool]
matrix:
  - { harness: claude-cli, model: default }
  - { harness: codex-cli,  model: default }
```

This expands to `scenarios × matrix × seeds` = 3 × 2 × 3 = **18 cells**, run strictly one at a
time (all of one scenario's cells before the next). Fields: `name`, `tier`, `seeds` (≥1),
`scenarios[]`, `matrix[]` (each a `harness`+`model`). Harness values are the same as `sreft run`:
`claude-cli`, `codex-cli`, `neutral-go`, `oracle`, `noop`.

## `sreft bench`

```sh
sreft bench suites/cli-sweep.yaml
```

- **Pre-flight**: checks Docker is reachable and removes any leftover `sreft-` containers, so a
  sweep fails (or starts clean) up front rather than midway.
- **Strictly sequential** with a teardown-and-clean between every cell — no two cells ever share
  the environment, so there are no container/port/project conflicts.
- **Per-cell resilience**: one cell erroring or timing out is recorded and the sweep continues; an
  agent that fails still yields a graded (usually NONE) result.
- Each cell is bounded by the scenario's wall-clock budget. `Ctrl-C` aborts after the current cell
  tears down.
- Writes everything into a **run directory** and prints the scorecard at the end.

Reference harnesses need no API key, so `sreft bench suites/smoke.yaml` (oracle + noop) is a fast,
free way to check the whole pipeline.

## A run (the output)

Each `bench` invocation produces a self-contained, reproducible artifact under `runs/`:

```
runs/<suite>-<UTC-timestamp>/
  manifest.json      the suite + git sha + tool version + timestamps + per-cell outcome
  <instance>/        one per cell (meta.json, observer.jsonl, transcript.jsonl, submission.json, score.json)
  scorecard.md       the aggregated scorecard
```

`manifest.json` is the run's disclosure record — enough to reproduce and interpret it (this is the
"disclose the harness" discipline the benchmark rests on). `runs/` is gitignored; commit a
`scorecard.md` deliberately when you want to publish a result.

## `sreft rescore` — re-grade for free

When the grader or a rubric changes, re-grade an existing run **from its saved artifacts** — no
agents re-run, no subscription spend:

```sh
sreft rescore <run-id>          # e.g. cli-sweep-20260709T190000Z
```

It re-runs the grader over each instance's `observer.jsonl` + `transcript.jsonl` + `submission.json`,
rewrites each `score.json`, and refreshes `manifest.json` + `scorecard.md`. This makes iterating on
scoring cheap and honest (it's how the conn-pool synonym-matcher fix was re-scored across a whole
run instantly).

## `sreft report`

```sh
sreft report --run <run-id>     # scorecard for a run
sreft report                    # scorecard for the flat results/ dir (ad-hoc `sreft run` output)
sreft report --run <id> --format json --out card.json
```

## Multiple seeds and reliability

`seeds: 3+` is where `pass^k` (all-k-resolve) and error bars become meaningful — with a single
seed per cell they're `±0.00`, which is honest but uninformative. Multi-seed is the biggest lever
on result quality and it's a one-line change in the suite.

## Not yet (deliberately)

- **Resumability** (`--resume` a partial run) — sequential sweeps are long; skipping completed
  cells after an interruption is a natural follow-on.
- **Parallelism** — everything is sequential by design for conflict-freedom; safe cross-scenario
  parallelism could come later.
