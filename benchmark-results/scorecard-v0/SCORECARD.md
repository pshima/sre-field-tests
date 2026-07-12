# SRE Field Tests — Scorecard v0

*First cross-model run — 2026-07-12. 108 instances · 8 contestants · 6 scenarios · $7.53 total (OpenRouter; CLIs on subscription).*

An interactive version is in [`scorecard.html`](scorecard.html).

## Standings

Mean composite over all 6 scenarios. `pass^k` = scenarios resolved on **every** seed (CLIs 3 seeds, raw models 2).

| # | Contestant | Type | SRE score | Full | pass^k | Diag | Remed | $/inc |
|---|---|---|--:|--:|:--:|--:|--:|--:|
| 1 | Codex | CLI | **0.92** | 15/18 | 4/6 | 0.95 | 1.00 | free |
| 2 | Claude Code | CLI | **0.83** | 13/18 | 3/6 | 0.94 | 0.89 | free |
| 3 | qwen/qwen3.7-max | raw | **0.62** | 6/12 | 2/6 | 0.55 | 0.75 | $0.056 |
| 4 | deepseek/deepseek-v4-pro | raw | **0.54** | 4/12 | 1/6 | 0.48 | 0.83 | $0.099 |
| 5 | x-ai/grok-4.5 | raw | **0.45** | 4/12 | 1/6 | 0.46 | 0.58 | $0.059 |
| 6 | z-ai/glm-4.5 | raw | **0.31** | 0/12 | 0/6 | 0.00 | 0.67 | $0.031 |
| 7 | moonshotai/kimi-k2.6 | raw | **0.26** | 1/12 | 0/6 | 0.08 | 0.42 | $0.078 |
| 8 | google/gemini-3.1-pro-preview | raw | **0.26** | 0/12 | 0/6 | 0.00 | 0.50 | $0.303 |

## Per-scenario composite

| Contestant | oom-killed | cpu-regex | conn-pool | bad-deploy | retry-storm | false-alarm |
|---|--:|--:|--:|--:|--:|--:|
| Codex | 0.96 | 1.00 | 0.92 | 0.98 | 1.00 | 0.67 |
| Claude Code | 1.00 | 0.81 | 1.00 | 1.00 | 0.67 | 0.50 |
| qwen/qwen3.7-max | 0.91 | 0.94 | 0.91 | 0.94 | 0.00 | 0.00 |
| deepseek/deepseek-v4-pro | 1.00 | 0.81 | 0.53 | 0.50 | 0.16 | 0.21 |
| x-ai/grok-4.5 | 0.50 | 0.50 | 0.00 | 0.97 | 0.58 | 0.16 |
| z-ai/glm-4.5 | 0.28 | 0.28 | 0.56 | 0.28 | 0.24 | 0.21 |
| moonshotai/kimi-k2.6 | 0.50 | 0.00 | 0.28 | 0.28 | 0.28 | 0.21 |
| google/gemini-3.1-pro-preview | 0.56 | 0.00 | 0.00 | 0.56 | 0.00 | 0.41 |

## Method

- **Grade the recovered state, not the transcript.** After the agent finishes, a separate observer keeps recording; the grader asserts whether the service is healthy and *sustained under load*, whether the fix holds, and whether any destructive/unnecessary action was taken.
- **Reliability is the headline** — `pass^k` (all k seeds resolve) over the mean.
- **Two harness classes:** `claude-cli` / `codex-cli` drive the incident inside their native coding CLIs on subscription (\$0 API); the six frontier models run through one neutral, fully-disclosed Go tool-loop (`neutral-go`) via OpenRouter, with prompt caching.

## Caveat — two diagnosis-0.00 rows under review

`glm-4.5` and `gemini-3.1-pro` scored **diagnosis 0.00 across all 12 instances** while still remediating. Two models pinned at a flat zero looks like a **submission-parsing artifact** (they likely don't emit the `submit` tool call cleanly through the neutral harness, so their RCAs never reach the grader) rather than a settled capability result. Treat those two scores as a floor pending a transcript audit.
