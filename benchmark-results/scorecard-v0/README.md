# Scorecard v0 — first cross-model benchmark run

The first real SRE Field Tests run: **8 agents × 6 scenarios**, 108 instances, 2026-07-12.

- **[`SCORECARD.md`](SCORECARD.md)** — the leaderboard + per-scenario table + method (start here).
- **[`scorecard.html`](scorecard.html)** — the interactive, self-contained scorecard (heatmap + cost/quality chart).
- **`leaderboard.json`** — the aggregated results, machine-readable.
- **`instances/<id>/`** — per-instance records: `meta.json` (config + token/$ usage), `score.json` (the grade), `submission.json` (the agent's RCA/postmortem), `transcript.jsonl` (normalized tool calls — what the agent actually ran).

## What's here vs. not

Each instance's raw **`observer.jsonl`** time-series (~160 MB across the run) is **excluded for size** — so full `sreft rescore` isn't possible from this committed copy. The grades, submissions, and tool-call transcripts are retained for audit. Regenerate the full streams with `sreft bench suites/subscription.yaml` / `suites/openrouter.yaml`.

## Run configuration

- **Subscription suite** (`suites/subscription.yaml`): `claude-cli`, `codex-cli` × 6 scenarios × 3 seeds — on the local Claude/Codex subscriptions (\$0 OpenRouter).
- **OpenRouter suite** (`suites/openrouter.yaml`): 6 frontier flagships (Gemini 3.1 Pro, Grok 4.5, DeepSeek V4 Pro, Kimi K2.6, Qwen3.7 Max, GLM 4.5) via the `neutral-go` harness × 6 scenarios × 2 seeds. Total OpenRouter cost: **\$7.53**.
- 20 of the raw-model cells hit the 20-minute agent wall-clock on the hardest scenarios (a legitimate NONE).
