# Positioning vs prior art

The AIOps/SRE benchmark space is more crowded than it looks. This project must be clear about
what already exists and where our contribution is genuinely novel. Full detail with citations is
in [RESEARCH.md](../RESEARCH.md) Part 1E; this is the summary.

## The existing benchmarks

| Benchmark | What it does | Scoring |
|---|---|---|
| **IBM ITBench** (ICML'25) | 59 SRE tasks on live Kubernetes incident snapshots | Precision at full recall — miss any true cause → 0.0. Frontier models < 50%. |
| **Microsoft AIOpsLab** (MLSys'25) | Full lifecycle (Detect→Localize→Diagnose→Mitigate) on real microservices (K8s+Helm) with a push-button fault generator | Success, TTD, TTM, efficiency ($/tokens/interactions) + LLM-judge. |
| **SREGym** (2026) | Live full-loop with kernel/hardware faults, compound/metastable failures | Up to 40% spread between agents. |
| **MicroRemed** | End-to-end remediation execution on Online Boutique/K8s | Remediation success rate. |
| **RCAEval** | 735 RCA failure cases across 3 demo apps | AC@k, Avg@k. |
| **OpenRCA** (ICLR'25) | Locate root-cause element from >68GB telemetry | Locate-the-injected-fault. |
| **Cloud-OpsBench** (2026) | 452 cases, deterministic replayable snapshots | Dual outcome (A@k) + process (trajectory) scoring. |

## What they share (and therefore leave open)

Nearly all of them are:

- **Cloud / Kubernetes-heavy** — require a cluster; nothing is laptop-runnable.
- **RCA-focused, remediation-light** — score *naming* the cause; only a few test *fixing* it, and
  scores there are the lowest in the field.
- **Reusing the same 4 demo apps** — Online Boutique / Sock Shop / Train Ticket / DeathStarBench.
- **Silent on safety** — almost none penalize destructive remediation, unnecessary change, or
  acting when nothing is wrong.
- **Rarely time- or cost-scored** — only ITBench-AA and AIOpsLab surface TTM / $-per-task.

## Our wedge

SRE Field Tests targets the intersection nobody occupies:

1. **Local-first / laptop-accessible.** Scenarios run in plain Docker (fault primitives are
   cgroups + stress-ng + tc/netem — no cluster). Cloud/Terraform tiers are optional, not required.
2. **Real remediation + MTTR**, state-verified — not just RCA identification.
3. **A genuine safety / blast-radius penalty**, including decoy "nothing is wrong" scenarios —
   the biggest open gap in the prior art.
4. **pass^k reliability + real error bars**, which even the model-card benchmarks skip.
5. **Full harness disclosure + retained transcripts** (HELM model), on a neutral leaderboard —
   avoiding the "99/100 self-reported" problem.

No existing benchmark combines AIOpsLab's lifecycle + ITBench's strict scoring + a genuine
time/cost/**safety** composite reported with pass^k — *and* runs on a laptop. That intersection
is both the gap and the reason this can become a credible new "SRE score" row.

## Roadmap items borrowed from the prior art

- Deterministic-snapshot tier (Cloud-OpsBench/ITBench) alongside live-injection, as a separate
  scorecard column suited to model-card tables.
- Held-out private split + canary GUID + scenario-refresh cadence (LiveBench) against contamination.
- Cost/efficiency scoring ($/incident, tool-call efficiency).
- The "monitoring depends on the failed thing" dimension (AWS/Slack/Datadog/Meta) — diagnose-while-blind.
- Native-CLI harness adapters for "model + its own harness" columns — **Claude Code and Codex are
  built and validated**; Gemini CLI is next.
