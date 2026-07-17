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
- **Cause-naming, not system-restoring** — they end at *naming* the cause (which element, which
  change) and most grade it over a **frozen snapshot**: the system under test isn't running, so
  nothing is actually fixed and no post-fix state exists to assert. A handful execute a fix, and
  their scores are the lowest in the field. We stand up a **live** failing stack and grade the
  **recovered state** — the failing check now passes and *stays* passing under load — not a named
  answer.
- **Artifact attribution, not mechanism diagnosis** — attributing a failure to an artifact in a
  static bundle (the guilty log line, span, or commit) is a different skill from reasoning about
  *what class of failure* a running system is exhibiting and why. We test the latter, on live
  behavior.
- **Reusing the same 4 demo apps** — Online Boutique / Sock Shop / Train Ticket / DeathStarBench.
- **Silent on safety, and blind to abstention** — almost none penalize destructive remediation or
  masking-not-fixing, and none score **abstention: knowing when *not* to act**. Because our system
  is live, a destructive remediation is *detectable and penalizable* (a static bundle can't be
  damaged); and a dedicated no-fault scenario scores whether an agent correctly changes **nothing**
  when nothing is actually broken — the "acting when nothing is wrong" failure mode, made
  first-class.
- **Rarely time- or cost-scored** — only ITBench-AA and AIOpsLab surface TTM / $-per-task.

## Our wedge

SRE Field Tests targets the intersection nobody occupies:

1. **Local-first / laptop-accessible.** Scenarios run in plain Docker (fault primitives are
   cgroups + stress-ng + tc/netem — no cluster). Cloud/Terraform tiers are optional, not required.
2. **Real remediation + MTTR**, state-verified on a live system — the fix must actually hold under
   load — not RCA identification over a frozen snapshot.
3. **A genuine safety composite** — destructive-action / blast-radius penalties *and* an
   unnecessary-change penalty, including a no-fault scenario where the correct move is to **abstain**.
   Knowing when not to act is the biggest open gap in the prior art, and only a live system can score it.
4. **Non-triviality by construction.** Deterministic non-LLM baselines (the reflexes an agent is
   tempted by — "just restart it", "just scale it") are run through the same grader and must *not*
   pass; a scenario a one-line policy solves measures nothing.
5. **pass^k reliability + real error bars**, plus **tokens and $-per-incident**, so the leaderboard
   shows a cost-vs-quality frontier, not a single self-reported pass@1.
6. **Full harness disclosure + retained transcripts** (HELM model), on a neutral leaderboard —
   avoiding the "99/100 self-reported" problem.

No existing benchmark combines AIOpsLab's lifecycle + ITBench's strict scoring + a genuine
time/cost/**safety** composite reported with pass^k — *and* runs on a laptop. That intersection
is both the gap and the reason this can become a credible new "SRE score" row.

## Roadmap items borrowed from the prior art

- Deterministic-snapshot tier (Cloud-OpsBench/ITBench) alongside live-injection, as a separate
  scorecard column suited to model-card tables.
- Held-out private split + canary GUID + scenario-refresh cadence (LiveBench) against contamination.
- Cost/efficiency scoring — **$/incident + token accounting are shipped** (with prompt caching);
  tool-call / interaction efficiency is next.
- The "monitoring depends on the failed thing" dimension (AWS/Slack/Datadog/Meta) — diagnose-while-blind.
- Native-CLI harness adapters for "model + its own harness" columns — **Claude Code and Codex are
  built and validated** (and run in scorecard v0); Gemini CLI is next.
