# Scorecard v0 — first cross-model run

The first real benchmark: **8 agents × 6 scenarios × 108 instances** (2026-07-12), graded on the
recovered system state, with pass^k reliability and real token/`$` cost.

- **[Live, interactive scorecard →](https://pshima.github.io/sre-field-tests/)** (heatmap + cost/quality frontier)
- **Committed source + per-instance records:** [`benchmark-results/scorecard-v0/`](../benchmark-results/scorecard-v0/)
  — `SCORECARD.md` (the tables + method), `scorecard.html`, `leaderboard.json`, and each instance's
  grade / submission / transcript. Re-gradeable with `sreft rescore`.

## Headline findings

- **The scaffold is the single biggest effect.** The native coding CLIs — **Codex 0.92**,
  **Claude Code 0.83** — tower over every raw frontier model (best: **Qwen3.7 Max 0.62**). Same-class
  models; a purpose-built harness drives Docker far better than a thin tool-loop.
- **The novel scenarios separate the field hardest.** `retry-storm` (metastable dependency overload)
  and `false-alarm` (abstention — don't act when nothing's wrong) crushed the raw models and even
  humbled the CLIs, exactly as designed.
- **Reliability is discriminating even at the top** — the best contestant hit pass^k on only 4 of 6
  scenarios.
- **Cost is honest and comparable across the per-token models** (spanning ~6×); the subscription
  CLIs carry no per-incident meter (a flat paid plan — *not* free). Total OpenRouter spend: **$7.53**.

## Reference gate (keyless, every scenario)

Independently of the model runs, every scenario ships an `oracle` (known-good fix) and relies on a
`noop` — the grader's correctness gate, **oracle → FULL / no-op → ZERO** — plus deterministic reflex
baselines (`always-restart`, `mask`) that must *not* pass. `false-alarm` inverts the gate: the
correct reference *abstains* and scores FULL. Reproduce with `sreft verify <scenario>` and
`sreft bench suites/baselines.yaml` (no API key). See [baselines.md](baselines.md).

## Caveat carried in v0

Two rows (`glm-4.5`, `gemini-3.1-pro`) originally scored diagnosis 0.00 because those models never
called the `submit` tool — a harness gap since fixed (the loop now forces a final submission when a
run ends without one; [`internal/agentloop/loop.go`](../internal/agentloop/loop.go)). Their v0
diagnosis numbers are a floor; a refreshed run will update them.
