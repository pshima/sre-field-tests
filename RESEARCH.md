# SRE Field Tests — Foundational Research

> **Purpose.** This document preserves the deep research that grounds the design of the
> SRE Field Tests benchmark. It was produced at project inception (2026-07-02) from four
> parallel deep-research passes, each fetching primary sources (arXiv papers, official model
> cards, benchmark repos, engineering blogs, incident post-mortems). Every major claim carries
> a source URL. **This is reference material, not a task list** — work is tracked in GitHub
> Issues. Treat dated/very-recent (2026) figures flagged "verify" as directional until
> re-checked against primary PDFs.

## Table of contents

1. [How LLM/AI benchmarks report & score — and the AIOps/SRE prior art](#part-1)
2. [SRE competency areas & the failure-class catalog](#part-2)
3. [Real incident post-mortems (VOID and beyond)](#part-3)
4. [Fault-injection, observer & harness engineering](#part-4)
5. [Synthesis — design decisions this research drove](#part-5)

---

<a name="part-1"></a>
# Part 1 — How LLM/AI Benchmarks Report & Score + Prior Art

The goal is an **"SRE score"** credible enough to appear in the benchmark tables labs publish
on model release. To do that we must understand (A) the standard suite, (B) scoring methods,
(C) agentic-benchmark design patterns, (D) reporting conventions, and — most important —
(E) the AIOps/SRE benchmarks that already exist in our exact space.

## A. The standard benchmark suite in frontier model cards (2024–2026)

The reasoning-model era (o1 onward) shifted the headline suite away from now-saturated
classics (MMLU, GSM8K, HumanEval, DROP) toward harder, held-out, or post-cutoff tests.

| Benchmark | Measures | Size | Score unit | Status |
|---|---|---|---|---|
| **MMLU** | Broad knowledge, 57 subjects | 15,908 MCQ (4-way) | Accuracy % (exact-match letter) | Saturated ~88–92%; ~29% items show contamination; effectively retired |
| **MMLU-Pro** | Harder, reasoning-heavier | ~12,000 MCQ (10-way) | Accuracy % (CoT) | 16–33 pt drop vs MMLU; more prompt-robust |
| **GPQA Diamond** | PhD-level science, "Google-proof" | **198** (of 448) | Accuracy % / pass@1 | Flagship hard-science; experts ~65–74%, non-experts ~34% |
| **SWE-bench** | Resolve real GitHub issues | 2,294 Python instances | **% resolved** (hidden tests) | Superseded by Verified |
| **SWE-bench Verified** | Human-validated subset | **500** (validated by 93 devs) | % resolved | Dominant coding metric 2024–26 |
| **HumanEval** | Python function synthesis | 164 problems | **pass@1** (functional) | Saturated ~90–99%, retired |
| **AIME 2024/2025** | Competition math | 15–30 problems | Accuracy %; pass@1 + cons@64; ±tools | Tiny N → high variance; moved to 2025 for post-cutoff cleanliness |
| **MATH / MATH-500** | Competition math w/ steps | 12,500 / 500 test | Accuracy % (symbolic EM) | Saturated ~95–99% |
| **MMMU** | College multimodal reasoning | ~11.5K (val ~900) | Accuracy % | Flagship visual, ~73–84% |
| **GSM8K** | Grade-school math | 1,000 test | Accuracy % (CoT) | Saturated ~99%; GSM1K showed 12–15% contamination drop |
| **HLE** (Humanity's Last Exam) | Frontier expert knowledge | ~2,500 | Accuracy % + calibration; ±tools | Newest, least-saturated; experts >90%, models low |
| **DROP** | Discrete reading reasoning | ~96,000 | **F1 + EM** | Saturated ~85–90% F1 |
| **IFEval** | Verifiable instruction-following | ~500 prompts | Accuracy, 4 variants, **code-verified** | Valued for mechanical objectivity |
| **MGSM** | Multilingual math | 250 × 10 languages | Accuracy % | Inherits GSM8K contamination |

**Sources:** MMLU [2009.03300](https://arxiv.org/abs/2009.03300) · MMLU-Pro [2406.01574](https://arxiv.org/abs/2406.01574) · GPQA [2311.12022](https://arxiv.org/abs/2311.12022) · SWE-bench [2310.06770](https://arxiv.org/abs/2310.06770) · SWE-bench Verified [openai.com](https://openai.com/index/introducing-swe-bench-verified/) · HumanEval [2107.03374](https://arxiv.org/abs/2107.03374) · o1/AIME [openai.com](https://openai.com/index/learning-to-reason-with-llms/) · MATH [2103.03874](https://arxiv.org/abs/2103.03874) · MMMU [2311.16502](https://arxiv.org/abs/2311.16502) · GSM8K [2110.14168](https://arxiv.org/abs/2110.14168), GSM1K [2405.00332](https://arxiv.org/pdf/2405.00332) · HLE [2501.14249](https://arxiv.org/abs/2501.14249) · DROP [1903.00161](https://arxiv.org/abs/1903.00161) · IFEval [2311.07911](https://arxiv.org/abs/2311.07911) · MGSM [2210.03057](https://arxiv.org/abs/2210.03057)

**Usage by lab:** OpenAI o1 introduced pass@1 + cons@64 on AIME ([learning-to-reason](https://openai.com/index/learning-to-reason-with-llms/)); GPT-5 system card reports AIME 2025 / GPQA Diamond / SWE-bench Verified / MMMU / HLE ([intro GPT-5](https://openai.com/index/introducing-gpt-5/)). Anthropic Claude 3→4.x moved to SWE-bench Verified (headline) + GPQA Diamond + AIME (±tools) + MMMU + HLE + Terminal-Bench ([Claude 4](https://www.anthropic.com/news/claude-4)). Google Gemini 2.5 puts the modern suite in one table, all labeled "single attempt / pass@1" ([2507.06261](https://arxiv.org/html/2507.06261v1)). Meta Llama 3 used the classic suite; Llama 4 shifted to MMLU-Pro/GPQA/MMMU/LiveCodeBench ([2407.21783](https://arxiv.org/pdf/2407.21783), [Llama 4](https://ai.meta.com/blog/llama-4-multimodal-intelligence/)).

**Structural takeaway:** the *unit* of a score is heterogeneous (accuracy, pass@1, resolve
rate, F1, medal tiers). Credible modern benchmarks share three traits — **held-out/human-
validated, post-training-cutoff, and hard enough to leave headroom.** An SRE score must hit
all three.

## B. Scoring methodologies and their trade-offs

- **pass@k (functional correctness).** From Codex/HumanEval: generate *n ≥ k* samples, count
  *c* correct, use the **unbiased estimator** pass@k = 𝔼[1 − C(n−c,k)/C(n,k)] in numerically
  stable product form, *not* the biased `1−(1−c/n)^k` ([2107.03374](https://arxiv.org/abs/2107.03374)).
  Good for machine-checkable tasks; **inflates apparent capability** at k>1 without an oracle
  to select the right sample. Optimal temperature rises with k (~0.2 for pass@1, ~0.8 for pass@100).
- **Resolve rate (SWE-bench).** Execution-based: a patch is resolved only if all FAIL_TO_PASS
  tests pass *and* all PASS_TO_PASS still pass. Most gaming-resistant; binary; only as good as
  the repo's test coverage.
- **Accuracy / exact-match.** Cheap, deterministic — but brittle: penalizes semantically-correct
  answers with different formatting/units; MC accuracy is inflated by a 1/n guess floor + letter
  bias. This brittleness is the core motivation for execution-based and judge-based scoring.
- **Elo / Bradley-Terry / win-rate (LMArena).** Humans vote between blind responses; moved from
  order-dependent Elo to **Bradley-Terry MLE** (order-independent), with **bootstrapped 95% CIs**
  and **style control** regressing out length + markdown (length coef ≈0.249 dominates)
  ([arena](https://lmsys.org/blog/2023-05-03-arena/), [style-control](https://lmsys.org/blog/2024-08-28-style-control/)).
  Relative-only, crowd-skewed, gameable ("Leaderboard Illusion," [2504.20879](https://arxiv.org/abs/2504.20879)).
- **LLM-as-judge (MT-Bench).** A strong judge hits **85% agreement** with humans (above the
  ~81% human-human rate), but carries **position bias** (GPT-4 65% consistency, Claude-v1
  23.8%), **verbosity bias** (GPT-3.5/Claude fooled 91.3% by padding; GPT-4 8.7%), and
  **self-preference bias** ([2306.05685](https://arxiv.org/abs/2306.05685)). Mitigations:
  swap-order-and-require-agreement, provide references, fixed rubrics. AlpacaEval is highly
  length-gameable (22.9%→64.3%) until length-controlled ([2404.04475](https://arxiv.org/html/2404.04475v1)).
- **Rubric / weighted / partial-credit.** Points per satisfied criterion; good for multi-step
  tasks; weights are arbitrary/gameable and inherit judge biases.
- **Self-consistency (cons@k) & best-of-n.** cons@k returns the majority answer over k chains —
  **deployable (no oracle)**; GSM8K PaLM-540B 56.5%→74.4% at N=40 ([2203.11171](https://arxiv.org/abs/2203.11171)).
  BoN picks highest-reward sample (prone to reward hacking). Both cost k× compute and inflate
  over pass@1 unless labeled.
- **pass^k (reliability, τ-bench).** Probability that **all** k trials of a task succeed —
  measures *consistency*, the opposite of pass@k. Drops sharply with k (GPT-4o retail ~50%
  pass^1 → ~25% pass^8) ([2406.12045](https://arxiv.org/abs/2406.12045)). **This is the metric
  most relevant to SRE**, where an agent that fixes an incident 1-in-4 times is dangerous.
- **Temperature.** Greedy (T=0) is reproducible, best for pass@1/EM; T>0 needed for pass@k/cons@k.
  A score is undefined without stating T, top-p, n, k, seed.

## C. Agentic benchmark design patterns (isolation, verification, partial credit)

The most useful synthesis for us. **Environment isolation ladder:**
- **Per-task Docker image:** SWE-bench (one image per instance at repo commit; ~120GB, ARM
  build traps); Terminal-Bench (Dockerfile + task.yaml + tests + oracle per task); MLE-bench
  (Docker + GPU, **internet disabled** to prevent leakage).
- **Multi-container service mesh:** WebArena self-hosts Magento/GitLab/Reddit/CMS in Docker,
  reset by container restart, offline by design ([2307.13854](https://arxiv.org/html/2307.13854v4));
  Cybench runs a **Kali container + task containers on a shared network** ([2408.08926](https://arxiv.org/abs/2408.08926)).
- **Full VM snapshots:** OSWorld (VMware/VirtualBox/Docker/AWS; revert to initial-state snapshot).
- **Pure data/state sandbox:** τ-bench (per-task database instance, no container).

*For SRE — running systems, networks, failures — we need the middle-to-heavy end:
multi-container topology with snapshot/reset (OSWorld VMs + WebArena Docker-mesh + Cybench
Docker-network).*

**Success verification spectrum:**
- **Deterministic tests:** SWE-bench FAIL_TO_PASS + PASS_TO_PASS; Terminal-Bench pytest/bash.
- **Execution-based state getters:** OSWorld runs "getter" utilities to pull ground-truth state,
  then "logical evaluators" allowing alternative correct paths (**134 unique eval functions / 369
  tasks**); τ-bench **compares final DB state to an annotated goal** + checks info communicated.
- **Flag/exact-match:** Cybench (unique flag), GAIA (**quasi-exact match** with type normalization,
  no LLM judge; [2311.12983](https://arxiv.org/abs/2311.12983)).
- **Human-relative graded:** MLE-bench maps to real Kaggle leaderboard → bronze/silver/gold.
- **LLM-judge:** WebArena uses fuzzy GPT-4 match only for the info-seeking subset.

*For SRE, copy OSWorld's getter+evaluator and τ-bench's goal-state diff: after the agent acts,
query the live system — service healthy? alert cleared? config correct? — vs expected state.*

**Partial credit.** Most benchmarks are binary per task. Two graded schemes:
- **Subtask decomposition (Cybench):** hard CTFs split into ordered subtasks; three metrics
  (unguided binary, fractional subtask %, subtask-guided); difficulty calibrated by human
  "first-solve time" (2 min–25 hrs). **Maps directly onto detect→diagnose→mitigate→resolve.**
- **Human-leaderboard tiers (MLE-bench).**

**Reliability & reproducibility.** τ-bench's pass^k and MLE-bench's **≥3 seeds, mean ± 1 SE**
address nondeterminism. Traps to design around: live-web drift (→ self-host/offline),
architecture-dependent Docker builds, simulator stochasticity, and **underspecified tasks /
flaky verification** — the entire reason SWE-bench Verified exists (93 devs found 38.3% of
samples underspecified, 61.1% had tests that could reject valid solutions).

## D. Reporting & presentation conventions

- **Table layout.** Vendor-produced tables, new model left, competitors adjacent, best bolded —
  *not independently audited*. On SWE-bench Verified, **99 of 100 leaderboard entries are
  self-reported**. Decisive methodology (trials, thinking mode, sampling) lives in **footnotes**.
- **Error bars — almost nobody reports them.** The corrective is Anthropic/Evan Miller's "Adding
  Error Bars to Evals" ([2411.00640](https://arxiv.org/abs/2411.00640)): (1) SE via CLT not
  Bernoulli for fractional scores (flags the Llama 3 report); (2) **clustered SEs** when questions
  group (3× larger than naive on DROP); (3) resample K per question; (4) compare via **paired
  question-level differences**; (5) power analysis → **"new evals should contain ≥ ~1,000
  questions"** to detect a 3% diff at 80% power. Report as mean with SE: "65.5% (0.7%)."
- **Trials/averaging.** o1 on AIME: 74% pass@1 / 83% cons@64 / 93% re-ranked-1000. Note
  "avg@k / cons@k" vs "pass@k" (any-of-k) vs "pass^k" (all-of-k) "tell opposite stories" as k
  grows ([Anthropic evals](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents)).
- **Harness disclosure — critical for agentic scores.** "The agent scaffold can move a score
  10–20 points without changing the model"; "a SWE-bench number is uninterpretable without the
  scaffold." Anthropic: "when we evaluate an agent, we're evaluating the harness *and* the
  model" (a CORE-Bench task jumped 42%→95% after fixing grader bugs).
- **Contamination.** Combated with **canary GUIDs** (BIG-bench's `26b5c67b…`, reproduced by
  GPT-4 base and Claude 3.5) and **live / time-segmented** benchmarks — LiveBench refreshes
  questions ([2406.19314](https://arxiv.org/abs/2406.19314)), LiveCodeBench date-stamps problems
  ([2403.07974](https://arxiv.org/abs/2403.07974)).
- **Saturation → harder benchmarks.** Lifecycle MMLU→MMLU-Pro→GPQA→HLE recurs. Independent
  re-runners (Epoch, Scale SEAL, HELM, LiveBench) exist because self-reported numbers don't
  reproduce — cautionary tale: **o3 on FrontierMath, implied >25% at launch vs Epoch's
  independent ~10%**, plus undisclosed OpenAI funding ([TechCrunch](https://techcrunch.com/2025/04/20/openais-o3-ai-model-scores-lower-on-a-benchmark-than-the-company-initially-implied/)).
  Stanford **HELM** is the transparency model: multi-metric, standardized prompts, all raw
  transcripts released ([HELM](https://crfm.stanford.edu/helm/)).

## E. Existing AIOps / SRE / incident benchmarks — the prior art we must not ignore

This is our exact space and it is more crowded than most realize. (2026-dated entries flagged
"verify"; core facts on ITBench, AIOpsLab, RCAEval, OpenRCA are solid.)

**Flagship agentic SRE benchmarks:**
- **IBM ITBench / ITBench-AA** — strongest prior art. IBM Research + UIUC, ICML 2025
  ([2502.05352](https://arxiv.org/abs/2502.05352), [repo](https://github.com/itbench-hub/ITBench)).
  Three domains (SRE/CISO/FinOps); **SRE = 59 tasks (40 public + 19 held-out)** on live
  Kubernetes incident snapshots with alerts/events/traces/metrics/logs/topology. Scoring:
  **average precision at full recall** — the agent submits suspected root-cause entities; **if
  any ground-truth cause is missed, score = 0.0**, else score = precision (penalizing
  over-investigation). All frontier models **< 50%**.
- **AIOpsLab (Microsoft)** — canonical full-lifecycle framework, MLSys 2025
  ([2501.06706](https://arxiv.org/abs/2501.06706), [repo](https://github.com/microsoft/AIOpsLab)).
  Cleanest task decomposition in the field: **Detection → Localization → Root-cause Diagnosis →
  Mitigation.** Deploys real microservices (DeathStarBench SocialNetwork/HotelReservation) on
  K8s+Helm with Prometheus/Jaeger/Filebeat and a **"push-button fault generator"** injecting
  faults at app/virtualization/config layers. Scores **Success, Time-to-Detect, Time-to-Mitigate,
  Efficiency** (interactions/tokens/$ per task) + LLM-judge. Mitigation ≫ harder than detection.
- **SREGym (2026, verify)** — live full-loop SRE with kernel/hardware faults, compound/concurrent
  drills, metastable/correlated failures; 90 problems; up to 40% spread between agents ([2605.07161](https://arxiv.org/abs/2605.07161)).
- **MicroRemed (verify)** — uniquely scores **executing remediation end-to-end** (not just naming
  the cause) on Online Boutique/K8s; remediation success rate ([repo](https://github.com/LLM4AIOps/MicroRemed)).

**RCA-focused benchmarks:**
- **RCAEval** — standard RCA dataset; **735 failure cases**, 11 fault types, on Online
  Boutique/Sock Shop/Train Ticket; metrics **AC@k and Avg@k** at service + metric granularity;
  15 baselines (best BARO Avg@5 ≈ 0.80) ([2412.17015](https://arxiv.org/abs/2412.17015)).
- **OpenRCA (Microsoft, ICLR 2025)** — 335 cases + >68GB telemetry; locate root-cause element
  from long-context telemetry ([repo](https://github.com/microsoft/OpenRCA)). Critique: tests
  "locate the injected fault," not true causal RCA.
- **Cloud-OpsBench (2026, verify)** — 452 cases / 40 root-cause types; a **"state-snapshot
  paradigm"** freezing incidents into deterministic replayable layers with mocked tool
  interfaces; notably **dual outcome-based (A@k) + process-based (trajectory alignment, tool
  relevance, operational robustness) scoring** ([2603.00468](https://arxiv.org/abs/2603.00468)).

**Knowledge/QA (non-agentic):** OpsEval (7,184 MCQ + 1,736 QA, EN/CN;
[repo](https://github.com/NetManAIOps/OpsEval-Datasets)); LogHub 2.0, LogEval, NetEval-Exam
([awesome-LLM-AIOps](https://github.com/Jun-jie-Huang/awesome-LLM-AIOps)).

**Foundational industry study:** Microsoft "Recommending Root-Cause and Mitigation Steps for
Cloud Incidents" (ICSE 2023, [2301.03797](https://arxiv.org/abs/2301.03797)) — >40,000 real
incidents; fine-tuned GPT-3.5 beat GPT-3 by +15.4% RCA / +11.9% mitigation; >70% of on-call
engineers rated recommendations useful. Vendor MTTR claims (AWS, Datadog Bits AI, Cleric, Azure
SRE Agent) are marketing with **no shared dataset**.

**Gaps a new SRE score can fill (our opportunity map):**
1. **RCA-heavy, remediation-light** — almost everything scores *identifying* the cause; only
   MicroRemed/SREGym/AIOpsLab-mitigation test *fixing* it, and scores there are lowest.
2. **No standard time-based scoring** — only AIOpsLab uses TTD/TTM.
3. **Reproducibility vs. fidelity unresolved** — snapshots deterministic but static; live systems
   realistic but hard to score.
4. **Narrow app/fault diversity** — everyone reuses Online Boutique/Sock Shop/Train
   Ticket/DeathStarBench; stateful DBs, queues, multi-region, serverless thin; compound/metastable
   only in SREGym.
5. **Weak safety / no-op scoring** — almost none penalize destructive remediation, alert fatigue,
   or acting when nothing is wrong.
6. **Single-incident, single-agent** — no concurrent incidents, escalation/paging, human-in-loop.
7. **Cost/efficiency rarely scored** — only ITBench-AA and AIOpsLab surface $/task.
8. **Observability-tool interaction not standardized** — agents get pre-dumped telemetry instead
   of having to *query* Prometheus/Grafana/Jaeger.
9. **Detection/triage/on-call under-tested** — most RCA benchmarks assume the incident is declared.

**Recommendations for a credible "SRE score":**
1. Adopt the **Detect → Localize → Diagnose → Mitigate/Resolve** lifecycle as the task spine
   with Cybench-style subtask credit; calibrate difficulty by human first-resolve time.
2. **Verify by system state, not text** (OSWorld getter+evaluator, τ-bench goal-diff; SRE analog
   of FAIL_TO_PASS/PASS_TO_PASS). Reserve LLM-judge for a labeled secondary "explanation quality."
3. **Make reliability the headline — report pass^k.** ≥3 seeds, mean ± 1 SE; follow Miller/Anthropic
   (≥1,000 instances, paired comparisons, real CIs).
4. **Score a composite no existing benchmark offers: correctness × time × cost × safety** —
   ITBench precision-at-full-recall for RCA + state-based mitigation; AIOpsLab TTD/TTM; $/incident
   + tool efficiency; and a **negative safety term** (destructive/unnecessary actions, decoy
   "nothing is wrong" scenarios) — our clearest novelty.
5. **Solve reproducibility-vs-fidelity explicitly** — two tiers: deterministic snapshot (for
   model-card tables) + live-injection (higher fidelity), reported as separate columns.
6. **Containerize like a service mesh; disclose the harness completely**; publish raw transcripts.
7. **Design against contamination/saturation from day one** — held-out private split, canary GUID,
   diversify beyond the 4 stock apps, refresh scenarios on a cadence; keep SOTA well under 100%.
8. **Present to slot into model-card tables** — one headline number "SRE-bench: XX% (±SE), pass^3"
   + sub-table (per-stage credit, MTTR, $/incident, safety-violation rate); run a neutral
   leaderboard; use an independent re-runner.

**Positioning bottom line:** No existing benchmark combines AIOpsLab's lifecycle + ITBench's
strict precision scoring + Cloud-OpsBench's dual outcome/process grading + a genuine
time/cost/**safety** composite reported with pass^k and real error bars — *and* nobody is
**local-first / laptop-accessible**. That intersection is our wedge.

---

<a name="part-2"></a>
# Part 2 — SRE Competency Areas & the Failure-Class Catalog

Synthesized from the Google SRE books (sre.google), Brendan Gregg (brendangregg.com), the AWS
Builders' Library, Kubernetes/Postgres/Confluent docs, and real post-mortems.

## A. SRE core competency areas, prioritized by prevalence

### Tier 1 — Foundational (touch nearly every incident)
1. **Observability & the golden signals.** Three frameworks an SRE must know: **Four Golden
   Signals** (Google, per-service: latency, traffic, errors, saturation), **RED** = Rate/Errors/
   Duration (per-service), **USE** = Utilization/Saturation/Errors (Gregg, per-resource). Rule:
   RED on services, USE on infra. Track latency as **percentile histograms, not averages**
   ("a slow error is worse than a fast error").
   [monitoring](https://sre.google/sre-book/monitoring-distributed-systems/) ·
   [RED](https://grafana.com/blog/the-red-method-how-to-instrument-your-services/) ·
   [USE](https://www.brendangregg.com/usemethod.html)
2. **Reliability measurement — SLIs / SLOs / SLAs + error budgets.** Define indicators from user
   needs, set achievable targets, run the error budget as a control loop that gates releases.
   [SLOs](https://sre.google/sre-book/service-level-objectives/) ·
   [risk](https://sre.google/sre-book/embracing-risk/) ·
   [workbook](https://sre.google/workbook/implementing-slos/)
3. **Linux systems debugging & performance.** The most transferable hands-on skill. USE method +
   Gregg's **60-second checklist** (`uptime`, `dmesg`, `vmstat 1`, `mpstat -P ALL 1`, `pidstat 1`,
   `iostat -xz 1`, `free -m`, `sar -n DEV/TCP 1`, `top`). Read `/proc`, cgroups, the OOM killer,
   load average (includes D-state I/O wait on Linux). Modern: eBPF/bpftrace, flame graphs, `ss`/`ip`.
   [60s](https://www.brendangregg.com/Articles/Netflix_Linux_Perf_Analysis_60s.pdf) ·
   [linuxperf](https://www.brendangregg.com/linuxperf.html)
4. **Debugging methodology under pressure.** Start from questions, not familiar tools. Triage
   first ("fly the plane"), hypothesize, bisect, suspect recent changes, one change at a time,
   correlation ≠ causation. Avoid anti-methods (streetlight, random-change, traffic-light).
   [troubleshooting](https://sre.google/sre-book/effective-troubleshooting/) ·
   [methodology](https://www.brendangregg.com/methodology.html)

### Tier 2 — Core operational
5. **Incident response.** ICS-derived roles: Incident Commander (coordinates, no remediation),
   Ops, Comms, Planning. [managing-incidents](https://sre.google/sre-book/managing-incidents/) ·
   [PagerDuty](https://response.pagerduty.com/)
6. **Networking debugging.** DNS → TCP → TLS → HTTP → LB + kernel plane (conntrack/NAT). `dig`,
   `curl -v`, `ss`, `tcpdump`, `openssl s_client`, `mtr`. Modern K8s traps: conntrack exhaustion,
   SNAT race → 1–3s latency, 5s DNS delay.
7. **Distributed systems failure modes.** Partial failure, cascading failure (positive feedback),
   gray failure (differential observability), retries/timeouts/jitter/idempotency, circuit
   breakers, backpressure/bounded queues, **metastable failure** (trigger vs sustaining effect).
   [cascading](https://sre.google/sre-book/addressing-cascading-failures/) ·
   [timeouts+backoff](https://d1.awsstatic.com/builderslibrary/pdfs/timeouts-retries-and-backoff-with-jitter.pdf) ·
   [metastable](https://sigops.org/s/conferences/hotos/2021/papers/hotos21-s11-bronson.pdf)
8. **Release engineering & progressive delivery.** CI/CD, canary/blue-green, hermetic builds,
   rollback discipline — because "bad deploy/config" is the #1 self-inflicted outage class.
   Correlate every metric shift with a change marker.

### Tier 3 — Important supporting
9. **Toil reduction / automation** (50% cap). 10. **Container/Kubernetes ops** (82% of container
users run K8s in prod). 11. **Database operations** (replication/failover, pooling, slow
queries/indexing/locking, expand-contract migrations). 12. **Postmortems / blameless culture.**
13. **Capacity planning & load testing.** 14. **On-call sustainability** (≤25% on-call, ≤2
incidents/shift). 15. **IaC / config management / GitOps** (declarative desired-state + drift).

**Cross-cutting shapes worth encoding:** declarative desired-state + reconciliation;
positive-feedback overload (cascading/metastable); methodology-beats-tools.

## B. Ranked catalog of common failure classes

Ranked by (frequency × reproducibility × measurability) = benchmark value. **Repro** = local
Docker/compose/kind vs needs cloud.

| # | Failure class | Freq | Modern | Repro | Key symptoms / signals to monitor |
|---|---|---|---|---|---|
| 1 | **TLS/SSL cert expiry** | Very high | Rising (90→47-day certs) | **Trivial, 1 container** | `openssl s_client` → `Verify return code: 10`; handshake fail + 5xx exactly at `notAfter`; days-until-expiry→0 |
| 2 | **OOM / OOMKilled (cgroup mem)** | Very high | Very current | **Trivial** (`-m 256m`) | Exit **137**; `dmesg` "Out of memory: Killed"; cgroup `memory.events` oom_kill; RSS→limit; `/proc/pressure/memory` |
| 3 | **Bad deploy / config change + rollback** | Very high (#1) | Very current | **Easy** (2 tags behind nginx) | Error/latency spike vs deploy marker, **by version** vs control; auto-pause/rollback |
| 4 | **Slow queries / missing index** | Very high | Universal | **Very easy** (1 DB) | `EXPLAIN ANALYZE` → `Seq Scan`; `pg_stat_statements` by total_exec_time; MySQL slow log |
| 5 | **Connection pool exhaustion** | Very high | Very current | **Easy** (compose app+PG) | Hikari "request timed out"; PG `FATAL: too many clients`; pool active==max & pending>0; latency↑ while DB CPU fine |
| 6 | **DB connection saturation (max_connections)** | Very high | Very current | **Trivial** (`max_connections=20`) | `FATAL: sorry, too many clients already`; count vs max; pile of `idle in transaction` |
| 7 | **Kafka consumer lag / queue buildup** | Very high | Very current | **Easy** (compose Kafka) | `records-lag-max` (JMX); `--describe` LAG; watch the **derivative**; rebalance storms |
| 8 | **CPU throttling (cgroup CFS quota)** | High | Very current | **Trivial** (`--cpus 0.5`) | `cpu.stat` nr_throttled/throttled_time; **p99 spikes with normal avg CPU**; cfs_throttled_periods_total |
| 9 | **CrashLoopBackOff (K8s)** | Very high | Very current | **Trivial** (kind) | `CrashLoopBackOff`, rising RESTARTS; backoff 10s→300s; describe Exit Code; `logs --previous`; exit 1/2/127/137 |
| 10 | **Thundering herd / cache stampede** | High | Very current | **Easy** (app+Redis+slow origin) | Origin QPS multiplier synced to TTL; cache-hit craters; sawtooth; per-key inflight recompute |
| 11 | **Retry storms / amplification** | Very high | Very current (metastable) | **Easy–moderate** | Attempts-per-request >1 & climbing; traffic stays elevated after trigger clears; goodput falls as QPS rises; 503/429 waves |
| 12 | **File descriptor exhaustion** | High | Current | **Easy** (`--ulimit nofile=1024`) | `Too many open files` (EMFILE); `ls /proc/PID/fd | wc -l`; `/proc/sys/fs/file-nr`; growing CLOSE_WAIT |
| 13 | **Deadlocks (DB)** | High (retry-masked) | Evergreen | **Easy, deterministic** | PG `deadlock detected` (`40P01`); MySQL err **1213**; `SHOW ENGINE INNODB STATUS` |
| 14 | **Lock contention (DB/mutex)** | Very high | Current | **Easy** | `pg_stat_activity` wait_event Lock/LWLock; Innodb_row_lock_waits; **p99↑ while CPU flat** |
| 15 | **N+1 queries (ORM)** | Very high | Modern | **Very easy** | Same normalized query with huge `calls`, low mean; burst of identical DB spans in one trace |
| 16 | **Rate limiting (429s)** | Very high | Very current | **Trivial** (nginx `limit_req`) | HTTP 429 + `Retry-After`; token-bucket refill; client backoff storms |
| 17 | **Load balancer misconfig (health checks)** | Very high | Modern | **Easy core** (envoy/haproxy) | 503 "no healthy upstream"; 502 (backend RST / keep-alive < LB idle); HealthyHostCount→0 |
| 18 | **Disk full / inode exhaustion** | Very high | High | **Trivial** (loopback fs) | `ENOSPC`; `df -h` Use% vs **`df -i` IUse%**; `lsof +L1` deleted-but-open; DiskPressure |
| 19 | **Tail latency / coordinated omission** | Very high | Very current | **Easy** (CO reproduces locally) | p99/p99.9 histograms; flat p99 from closed-loop generator = CO artifact (ScyllaDB 249µs vs 665ms) |
| 20 | **Memory leak (app heap)** | Very high | Current | **Moderate** (needs load) | RSS monotonic → OOMKilled; JVM heap plateau near ceiling; `jmap`+MAT; NMT |
| 21 | **Goroutine / thread leak** | High (subtle) | Very current | **Easy–moderate** | `go_goroutines` monotonic↑; `/debug/pprof/goroutine?debug=2`; jstack BLOCKED/WAITING |
| 22 | **Thread pool exhaustion** | High | Current | **Easy** (fixed pool + hung dep) | Timeouts **while CPU idle**; active==max, queue rising, RejectedExecutionException |
| 23 | **Node pressure (Mem/Disk/PID)** | High | Very current | **Moderate** (kind) | describe node Conditions True; eviction signals memory.available/nodefs.available/pid.available |
| 24 | **Pod evictions / preemption** | High | Very current | **Moderate** (kind + PriorityClass) | `Evicted`; kubelet ranks requests→Priority→QoS (BestEffort first) |
| 25 | **Replication lag** | Very high | Very current | **Easy** (compose PG primary+replica) | `pg_stat_replication` sent vs replay LSN; `now()-pg_last_xact_replay_timestamp()`; stale reads |
| 26 | **DNS failures** | Extremely high | Very current | **Client-side easy** | `dig` SERVFAIL/NXDOMAIN; `curl` "Could not resolve host"; retry-driven query surge (Cloudflare ~30×) |
| 27 | **Ephemeral port / TIME_WAIT exhaustion** | Med-high | High (tcp_tw_recycle removed 4.12) | **Moderate** (shrink port range) | `EADDRNOTAVAIL`; `ss -tan state time-wait | wc -l`; conntrack table full |
| 28 | **Hot partition / hot key** | High | Very current | **Easy via Kafka** | One partition's lag/CPU diverges; DynamoDB ThrottledRequests uneven per-partition |
| 29 | **Feature flags gone wrong** | High, rising | Very current | **Easy core** (offline SDK flip) | Behavior/error change with **NO deploy**; align to flag audit-log event |
| 30 | **Failed failover / split-brain** | Low (high-sev) | Current | **Hard** — demonstrable, not faithful | Two nodes `pg_is_in_recovery()=false`; Patroni two leaders; needs partition + STONITH |
| 31 | **Cascading failure (composite)** | High (as outcome) | Very current | **Hard, approximable** | Bimodal latency; load redistribution as replicas die; crash-restart loops; goodput ≪ offered |

## C. Recommended first 8–12 scenarios (common + reproducible + measurable)

Suggested build sequence. Each has a deterministic, gradeable "aha" signal:
1. **TLS certificate expiry** — cleanest/most deterministic of all.
2. **OOMKilled container** — single most common container crash. **(chosen as scenario #1)**
3. **Bad deploy + rollback** — #1 real-world outage class; tests change-correlation.
4. **Slow query / missing index** — highest-fidelity DB scenario.
5. **Connection pool exhaustion** — distinguishes pool sizing from DB load.
6. **CPU throttling (cgroup CFS)** — p99 spikes with normal avg CPU; rewards the right model.
7. **CrashLoopBackOff** — canonical K8s debugging loop.
8. **Kafka consumer lag** — #1 streaming health signal.
9. **Thundering herd / cache stampede** — distributed-systems reasoning, fully local.
10. **Retry storm / metastable overload** — most important modern outage shape; goodput-vs-offered.
11. **File descriptor leak** — classic leak with an exact signal.
12. **DB deadlock** — deterministic and self-contained.

Strong bench choices: **Replication lag**, **LB 503 / health-check misconfig**. Deliberately
deferred (low reproducibility): **split-brain/failover** and **full cascading failure** (need
real partition + STONITH / fleet scale); **DynamoDB hot partitions** (need AWS — same class via
Kafka keyed producer locally); **Meta-style BGP-withdrawal DNS** (only the client symptom yields
locally).

## D. Signals the observer must capture (per scenario, some subset)

Process/cgroup **CPU% + throttle counters**, **memory RSS + cgroup oom events**, **fd counts**
(`/proc/PID/fd`), **socket state distribution** (`ss`), **latency percentiles as histograms**,
**error/HTTP-status rates** (esp. 429/502/503), **queue depth / consumer lag**, **DB pool +
`pg_stat_activity` state**, **replication LSN lag**, and **structured log lines / exit codes**
(137, `40P01`, `SERVFAIL`, `ENOSPC`, "Too many open files"). These are exactly the fields an AI
SRE agent must both read and be judged against.

---

<a name="part-3"></a>
# Part 3 — Real Incident Post-Mortems (VOID and beyond)

## A. The VOID (Verica Open Incident Database)

**What / who:** a public collection of publicly available software incident reports (RCAs,
status-page postmortems, blog writeups), created and led by **Courtney Nash** at **Verica**,
launched **October 2021**.
[database](https://www.thevoid.community/database) ·
[launch](https://www.businesswire.com/news/home/20211005005193/en/)

**Size:** grew from ~1,800 reports (2021) to **~10,000 incidents / ~600 companies** by the Dec
2022 report; the Feb 2024 report draws on **>10,000 reports**. MAANG down to startups.
[InfoQ](https://www.infoq.com/articles/analyzing-incident-data/)

**Data access (important):** browse/search UI at thevoid.community/database only — **no public
bulk-download and no public API** surfaced. You contribute by dropping a link. Licensing
unconfirmed (JS-rendered page). **Practically: curate from the underlying public source reports,
not a VOID export.**

**Key published findings:**
1. **MTTR is not a viable reliability metric** — incident duration is **positively skewed /
   heavy-tailed** ("gray data"), so mean/median mislead; a 10% duration reduction produced no
   consistent MTTR change. (They say "heavy-tailed/positively-skewed"; the specific term
   "Weibull" was **not** confirmed — treat as unverified.)
2. **No correlation between duration and severity.**
3. **~53% of incidents resolved within ~2 hours**, with a long tail.
4. **"Shallow vs deep" data** (Allspaw) — argues against shallow metrics (MTTR, counts) toward
   SLOs, cost-of-coordination, qualitative review.
5. **Near misses are <1% of reports** — underreported, highest-value/lowest-blame learning.
6. **Only ~25% of reports name a "root cause"**; "human error" over-cited → push **"contributing
   factors" over "root cause."**
7. **2024 report (automation):** in **~75%** of ~200 automation incidents, humans had to
   intervene — automation often *exacerbated* incidents. Six archetypes: Sentinel, Gremlin,
   Meddler, Unreliable Narrator, Spectator, Action Item.
[reports](https://www.thevoid.community/report) ·
[metrics](https://www.infoq.com/articles/incident-metrics-void/) ·
[SRECon](https://www.usenix.org/conference/srecon22americas/presentation/nash)

**Benchmark takeaway:** use the VOID as a *methodological* guide (how to frame/select incidents,
what to measure) and a pointer-list to primary sources — not as a labeled dataset. Its anti-MTTR
stance → score **diagnosis/remediation quality**, not just time-to-fix.

## B. Other post-mortem sources & access

| Source | URL | What / access | Themes |
|---|---|---|---|
| **danluu/post-mortems** | [github](https://github.com/danluu/post-mortems) | Plain-text link-list; clone/read/PR; ~100+ | Config errors, hardware/power, races, clock/leap, database |
| **k8s.af / kubernetes-failure-stories** | [k8s.af](https://k8s.af) · [codeberg](https://codeberg.org/hjacobs/kubernetes-failure-stories) | ~50–58 stories 2017–23 | **DNS** (most common), CPU throttling (CFS), OOM, conntrack/SNAT, control-plane/etcd, cascades, upgrades |
| **sadservers.com** | [scenarios](https://sadservers.com/scenarios) | **Closest existing model** — throwaway VMs pre-broken with one fault; SSH in, fix in-session, auto-graded; ~100+ | Full disk, DNS/ports, nginx/HAProxy/Caddy, PG/MySQL won't start, permissions, systemd/cron, OOM/cgroups, containers/k8s, expired SSL (Geneva), forensics |

**Academic datasets/papers:**
- **Oppenheimer et al. 2003, "Why Do Internet Services Fail…"** (USITS) — 3 large services;
  operator error is the leading cause in 2 of 3 and dominates repair time.
  [PDF](https://pages.cs.wisc.edu/~remzi/Classes/739/Fall2018/Papers/oppenheimer.pdf)
- **Gunawi et al. 2016, "Why Does the Cloud Stop Computing?"** (SoCC) — **597 outages / 32
  services / 1,247 public reports / 2009–2015**; taxonomy by root cause/duration/impact — strong
  template. [PDF](https://ucare.cs.uchicago.edu/pdf/socc16-cos.pdf)
- **Gunawi et al. 2014, "What Bugs Live in the Cloud?"** — ~3,655 issues / 6 distributed systems.
  [PDF](https://ucare.cs.uchicago.edu/pdf/socc14-cbs.pdf)
- **Basiri et al. 2016, "Chaos Engineering"** (Netflix, IEEE Software).
  [PDF](https://arxiv.org/pdf/1702.05843)

## C. Most common real-world root causes (ranked, with evidence)

For **software/service incidents** (our target):

| Rank | Category | Frequency evidence |
|---|---|---|
| 1 | **Change to a live system** (deploy + config) | **~70% of outages** (Google SRE); UPGRADE+CONFIG top disclosed (Gunawi); config = 62% of IT-software outages (Uptime) |
| 2 | **Configuration error** (subset) | **>50%–~100%** of operator errors were config (Oppenheimer); ~50% of DNS outages |
| 3 | **Human/operator error** | Leading in 2 of 3 services, **~33–36%** (Oppenheimer) |
| 4 | **Software bugs/defects** | **~40%** of Azure high-sev; 25–27% (Oppenheimer) |
| 5 | **Network / DNS / connectivity** | **76%** of one service's failures (Oppenheimer); NETWORK #2 (Gunawi) |
| 6 | **Dependency / third-party / cascading** | **75%** name it the most common trigger (Cockroach State of Resilience 2025) |
| 7 | **Capacity / resource exhaustion / load** | LOAD a named Gunawi category |
| 8 | **Hardware** | 25% (Oppenheimer Online), 4–10% elsewhere |
| 9 | **Expired TLS certificates** | **88% of orgs** hit ≥once (Keyfactor 2024); ~81% of cert outages from expiry |
| 10 | **Power / environmental** | ~6% of cloud outages (Gunawi) — but **#1 (~54%) at the physical DC layer** (Uptime) |

**Anchor stat:** Google SRE's "**~70% of outages are due to changes in a live system**" (SRE
book Ch.1). Caveats: internal, counts *triggers*; Gunawi covers only the ~40% that disclosed a
cause; the VOID's thesis is that clean single-cause percentages oversimplify. **Consistent signal:
change (deploy/config) + human/operator error dominate software outages.**

## D. Curated reference incidents for reproducible scenarios

Ordered cleanest-to-stage → systemic.

1. **Cloudflare regex CPU exhaustion (Jul 2 2019) + Stack Overflow regex (Jul 20 2016).** A WAF
   rule with catastrophic-backtracking regex (`.*.*=.*`) pinned every CPU globally, 502s, ~27
   min. SO variant: a malformed post → O(n²) trim regex → CPU spike; the LB health check hit the
   homepage → LB evicted all servers → full outage. **Repro: trivial** — tiny HTTP service applies
   `(a+)+$` to input; one crafted string pins CPU. Point a real LB health check at it to reproduce
   the SO eviction cascade.
   [CF](https://blog.cloudflare.com/details-of-the-cloudflare-outage-on-july-2-2019/) ·
   [SO](https://stackstatus.net/post/147710624694/outage-postmortem-july-20-2016)
2. **TLS cert expiry (MS Teams Feb 3 2020; Ericsson→O2/SoftBank Dec 6 2018; Spotify May 2022).**
   Cert hit `notAfter`; dependents refuse handshake / fail-safe halt (~30M O2 users). **Repro:
   very high/deterministic** — serve HTTPS with a cert expiring seconds out (or advance clock).
   [Teams](https://www.theregister.com/2020/02/03/microsoft_teams_down/) ·
   [O2](https://techcrunch.com/2018/12/07/heres-what-caused-yesterdays-o2-and-softbank-outages/)
3. **Disk / log partition full (`ENOSPC`).** Unbounded logs fill `/var/log` or `/`; daemon can't
   write; can't restart. **Repro: trivial** — mount 100MB tmpfs, run a verbose logger / `fallocate`.
   [writeup](https://russell.ballestrini.net/build-server-postmortem-disk-full/)
4. **Connection-pool exhaustion / DB `max_connections`.** Fixed pool + surge + slow queries →
   every connection checked out → requests block/timeout; DB side `FATAL: too many clients`.
   Non-linear cliff. **Repro: high** — PG `max_connections=20`, small pool, `SELECT pg_sleep(5)`,
   concurrency > pool. Fix demo: bounded queue / PgBouncer.
   [writeup](https://www.c-sharpcorner.com/article/postgresql-connection-pool-exhaustion-lessons-from-a-production-outage/)
5. **DB deadlock / lock-contention pileup.** Transactions acquire rows in different orders →
   circular wait → `deadlock detected` + victim rollback (Clerk 2025 variant: PG upgrade removed
   an implicit throttle). **Repro: high** — two loops updating (row1,row2) vs (row2,row1). Fix:
   sort keys before update.
   [Clerk](https://clerk.com/blog/2025-09-18-database-incident-postmortem) ·
   [GitHub](https://github.blog/2016-02-04-january-28th-incident-report/)
6. **Memory leak → OOM killer → cascading restart.** Leaking process grows until OOM-killed (exit
   137), restarts, leaks again; in a fleet each death dumps load on survivors. **Repro: high** —
   `docker run -m 128m` a leaky service; for cascade, 3 behind an LB, kill one. **(basis for
   scenario #1)** [GKE OOM](https://docs.cloud.google.com/kubernetes-engine/docs/troubleshooting/oom-events)
7. **Knight Capital (Aug 1 2012) — bad deploy / repurposed feature flag ($440M).** New code
   deployed to 7 of 8 servers; the 8th kept old code; a reused flag reactivated defunct "Power
   Peg" logic → ~4M erroneous orders in 45 min. **Repro: very high** — N containers behind an LB;
   ship to N-1; env flag means "enable loop" in old build vs "new path" in new; flip it.
   [writeup](https://dougseven.com/2014/04/17/knightmare-a-devops-cautionary-tale/)
8. **AWS DynamoDB retry storm (Sep 20 2015) — thundering herd.** Network blip cut storage from
   metadata; synchronized re-requests exceeded timeout; servers took themselves offline and
   retried in a loop; ~55% failure, ~5h. AWS had to *pause* metadata requests. **Repro: high** —
   metadata container with a fixed budget + M workers retrying with no backoff; kill/restore
   metadata. Fix: backoff+jitter+budgets. [message](https://aws.amazon.com/message/5467D2/)
9. **Cascading failure from retries with no backoff/jitter (Google SRE Ch.22).** Retry
   amplification (4×4×4 = 64 attempts/action), synchronized retries, overload feedback. **Repro:
   ideal A/B** — Config A (immediate retries) fails to recover; Config B (backoff+jitter+budget)
   self-heals. [SRE](https://sre.google/sre-book/addressing-cascading-failures/)
10. **AWS S3 typo (Feb 28 2017) — human command error.** A mistyped playbook arg removed far more
    servers than intended (index + placement subsystems) → multi-hour restart; AWS's own status
    dashboard depended on S3. **Repro: moderate** — a "remove N servers" admin command lacking
    bounds-checking; tests whether the agent adds guardrails. [message](https://aws.amazon.com/message/41926/)
11. **GitHub network partition → MySQL split-brain (Oct 21 2018).** A 43s connectivity loss →
    Orchestrator failed primaries to West Coast; on return both coasts held un-replicated writes →
    24h11m degraded, prioritizing integrity. **Repro: moderate** — two MySQL + orchestrator +
    `iptables` partition. [analysis](https://github.blog/2018-10-30-oct21-post-incident-analysis/)
12. **Meta BGP+DNS withdrawal (Oct 4 2021).** A maintenance command disconnected the backbone;
    DNS servers withdrew their own routes when they lost backbone reachability → all domains
    unresolvable ~6h though servers were fine. **Repro: high for the DNS half** — authoritative
    DNS + resolver + clients; kill/blackhole DNS or have it deregister on a failed health check.
    [FB](https://engineering.fb.com/2021/10/05/networking-traffic/outage-details/) ·
    [CF](https://blog.cloudflare.com/october-2021-facebook-outage/)

**More breadth:** Fastly latent bug (Jun 8 2021), Datadog systemd/Cilium (Mar 8 2023), Slack
post-holiday thundering herd + open-files limit (Jan 4 2021), GCP network control-plane (Jun 2
2019), Cloudflare BGP misordering (Jun 21 2022).

**Design notes:**
- **Cleanest single-container** (best for diagnosis+remediation scoring): regex CPU (#1), cert
  expiry (#2), disk full (#3), connection-pool (#4), deadlock (#5) — map ~1:1 onto sadservers.
- **Multi-container cascades** (systemic reasoning): OOM cascade (#6), Knight stale-fleet (#7),
  retry storm (#8/#9), DNS withdrawal (#12), split-brain (#11).
- **Recurring meta-pattern worth encoding as a scenario dimension:** *the monitoring/status system
  depended on the thing that failed* — AWS (dashboard on S3), Slack (dashboards dark), Datadog
  (monitors), Meta (internal tools via DNS). Tests whether an agent can diagnose while blind.
- Borrow **Gunawi 2016's taxonomy** to categorize/weight scenarios; heed the **VOID** to score
  diagnosis/remediation quality over MTTR.

---

<a name="part-4"></a>
# Part 4 — Fault-Injection, Observer & Harness Engineering

**Core finding:** the higher-level chaos tools (Chaos Mesh, Litmus, Gremlin) are orchestration
layers that delegate to the same three Linux primitives — **tc/netem**, **stress-ng**, and
**cgroups** — plus the Docker API for process kill. For a local-first Go benchmark, build on the
primitives directly and study **Pumba** (Go) as the reference wrapper.

## A. Fault-injection building blocks (ranked, local-first, Go-friendly)

| Rank | Tool | Mechanism | Faults | Local Docker, no k8s? |
|---|---|---|---|---|
| 1 | **stress-ng** | userland workers doing real syscalls/instructions | CPU, mem (`--vm`), IO | Yes |
| 2 | **tc/netem** | kernel egress qdisc on iface/netns | latency+jitter, loss, corrupt, dup, reorder, rate | Yes (needs `NET_ADMIN`) |
| 3 | **Pumba** | Go tool wrapping Docker API + tc/netem + stress-ng | process kill/stop/pause/rm/restart, netem, stress | **Yes — core use case** |
| 4 | **cgroups v2** (Docker API) | kernel resource controllers | CPU cap (`cpu.max`), mem cap (`memory.max`→OOM), IO cap | Yes (`--cpus`,`--memory`,`--device-write-bps`) |
| 5 | **Toxiproxy** | userland L4 TCP proxy (Go), API :8474 | latency, bandwidth, timeout, `reset_peer` (RST), slow_close, slicer — **zero privileges** | Yes |
| 6 | **Docker API** | `ContainerKill`/`Stop`/`Pause` | process-kill class | Yes |

Lower priority locally: **Chaos Mesh** (k8s CRDs; `chaosd` standalone wraps stress-ng),
**LitmusChaos** (k8s-operator only), **Chaos Toolkit** (a Python *orchestrator*, useful only as a
declarative scenario format), **Gremlin** (closed SaaS + phones home → breaks reproducibility).

**Recommended injection layer:** **stress-ng + tc/netem + cgroups + Docker-API-kill wrapped in
Go**, plus **Toxiproxy** for privilege-free TCP faults (covers connection-oriented faults tc
can't; tc covers L3 packet faults Toxiproxy can't). Read Pumba's source for how to attach into
container namespaces (it auto-provisions a `--tc-image` sidekick sharing the target's netns so app
images stay clean).

**Sources:** [Pumba](https://github.com/alexei-led/pumba) ·
[stress-ng](https://manpages.ubuntu.com/manpages/focal/man1/stress-ng.1.html) ·
[tc-netem](https://man7.org/linux/man-pages/man8/tc-netem.8.html) ·
[cgroup v2](https://docs.kernel.org/admin-guide/cgroup-v2.html) ·
[Toxiproxy](https://github.com/Shopify/toxiproxy) · [Chaos Mesh](https://chaos-mesh.org/docs/basic-features/)

## B. Observer / monitoring agent design under degradation

**Governing principle:** a `/proc` + `/sys`-first core is the resilient baseline — few deps,
minimal privilege, degrades gracefully. eBPF is *optional enrichment*, not the last line of
defense (kernel-version/privilege/complexity surface + its own load-time failure under memory
pressure).

**Go runtime robustness under pressure:**
- Set **`GOMEMLIMIT`** (soft limit, Go 1.19+) *and* `GOGC=off` — eliminates steady-state GC with a
  safety valve; the runtime caps GC CPU at ~50% to avoid a death spiral. It's *soft* — "eliminate
  OOM in 100% of cases" is an explicit non-goal.
  [proposal](https://github.com/golang/proposal/blob/master/design/48409-soft-memory-limit.md)
- **`sync.Pool`** for hot-path buffers (~0 allocs/op) — pooled objects can be dropped on any GC
  and must be reset before reuse. [pool](https://pkg.go.dev/sync#Pool)
- **Fixed/bounded worker pool**, never unbounded goroutines.
- **`RLIMIT_NOFILE`:** raise your own soft limit at startup; **pre-open a reserved fd** you can
  close to guarantee a write path under fd exhaustion (standard "spare fd" idiom).
  [getrlimit](https://man7.org/linux/man-pages/man2/getrlimit.2.html)
- Consider `mlockall` to avoid being swapped out.
- **node_exporter lessons:** on-demand reads of `/proc`+`/sys`; `tcpstat` "has potential
  performance issues in high load"; a `stat()` on a hung NFS mount can stall a collector; watch
  `scrape_duration_seconds`. [node_exporter](https://github.com/prometheus/node_exporter)

**Metric interfaces & trade-offs:**
- **`/proc` + `/sys`:** cheapest, most portable, mostly no privileges. Cost is *enumeration*
  (per-PID/per-socket). System-wide aggregate counters (`/proc/stat`, `/proc/meminfo`,
  `/proc/loadavg`, `/proc/net/dev`) are tiny fixed reads — cheapest, most robust under load.
- **netlink `sock_diag`:** parsing `/proc/net/tcp` "can be very expensive" at high socket counts
  (~50k sockets can blow a 1s budget); `sock_diag` is the efficient replacement (why `ss` beats
  `netstat`). [sock_diag](https://man7.org/linux/man-pages/man7/sock_diag.7.html)
- **netlink `taskstats`:** efficient per-task stats + delay accounting, avoids walking every
  `/proc/<pid>`. [taskstats](https://docs.kernel.org/accounting/taskstats.html)
- **eBPF:** in-kernel aggregation, captures short-lived events polling misses; costs complexity +
  privileges (`CAP_SYS_ADMIN`/`CAP_BPF`), kernels ≥4.4, amd64/arm64. [ebpf](https://github.com/cilium/ebpf)

**Poll /proc vs eBPF for a degradation-survivor:** poll `/proc` for the resilient core; mitigate
parse-allocation GC pressure with pooled buffers; guard against hung-mount/huge-socket-table
stalls with timeouts. Use netlink for enumeration-heavy metrics (sockets, per-process); reserve
eBPF as optional enrichment.

**What to capture (Gregg USE method, [use-linux](https://www.brendangregg.com/USEmethod/use-linux.html)):**
CPU util (`/proc/stat`) + saturation (run-queue, `schedstat`); memory util (`/proc/meminfo`) +
saturation (swap si/so, page-scan) + **OOM-kill events (`dmesg`)**; load average
(`/proc/loadavg`); disk `%util`/`await`/queue (`/sys` block stats); network bytes +
drop/fifo/retransmit (`/proc/net/dev`, `netstat -s`); **file descriptors — system-wide
`/proc/sys/fs/file-nr`, per-process `/proc/<pid>/fd/` count + `/proc/<pid>/limits`**; per-process
via `taskstats`/`pidstat`.

## C. Result data format (append-only, crash-safe, analyzable)

- **JSON Lines** ([jsonlines.org](https://jsonlines.org/)): one JSON value per `\n`. Append
  crash-safety is a property of your *write discipline*, not the format: `O_APPEND`, one complete
  `record\n` per write, `fsync` per batch, discard a torn trailing line on read. Human-readable,
  zero-dependency.
- **OpenTelemetry file exporter** ([spec](https://opentelemetry.io/docs/specs/otel/protocol/file-exporter/)):
  the local file format *is* JSONL — but **Development status**, **no ordering/monotonic-timestamp
  guarantee**. Borrow the OTel semantic-convention vocabulary (`system.cpu.utilization`,
  `system.memory.usage`, `system.network.*`; events as log records) inside plain JSONL — but
  treat system-metrics conventions as Development-stage.
- **Prometheus:** exposition is a scrape text format (no append/crash semantics); remote-write
  pushes to a remote; the crash-safe TSDB WAL is internal. Only if you run Prometheus.
- **Parquet** ([format](https://parquet.apache.org/docs/file-format/)): **immutable, not
  appendable, not crash-safe as a growing file** (footer written last). Excellent for *later
  analysis*. Use in a batch/rotate pattern: one complete `.parquet` per interval, closed. Go:
  `parquet-go/parquet-go`, Apache Arrow Go.
- **SQLite WAL** ([wal](https://sqlite.org/wal.html)): ACID with `synchronous=FULL`; `NORMAL`
  faster, corruption-safe, can lose last few commits on power loss. Same-host only. Drivers:
  `mattn/go-sqlite3` (CGO, fast) vs `modernc.org/sqlite` (pure-Go, cross-compiles, ~2× slower).

**Recommendation — hybrid:** (1) **hot path (during scenario, host possibly degraded): fsync'd
JSONL, OTel-named fields** — simplest crash-recoverable append stream, zero deps, no CGO; correct
precisely because the observer must survive degradation. (2) **cold path (analysis): rotate JSONL
→ immutable Parquet snapshots**, or load into SQLite. **DuckDB** reads JSONL, Parquet, *and*
SQLite uniformly via SQL — storage choice doesn't lock analysis choice, and conversion can be
deferred. If you'd rather have indexed transactional writes on the hot path, use **SQLite/WAL**
with the pure-Go driver to keep `CGO_ENABLED=0` — but JSONL is the safer default for the
observer-under-degradation role.

## D. How existing agentic benchmarks expose environments & verify success

Convergent pattern: **Docker-isolated environment + a command/tool channel + programmatic
state-based verification** — never judging the transcript. Copy this.

- **SWE-bench / Verified** ([paper](https://arxiv.org/abs/2310.06770),
  [harness](https://www.swebench.com/SWE-bench/reference/harness/)): task = JSON instance
  (`instance_id`, `repo`, `base_commit`, `problem_statement`, gold `patch`, `test_patch`,
  `FAIL_TO_PASS`/`PASS_TO_PASS`). 3-layer Docker images. Verify: apply model patch → apply
  test_patch → run tests → **resolved only if F2P ratio = 1 AND P2P ratio = 1**. Non-interactive.
- **Terminal-Bench** ([paper](https://arxiv.org/html/2601.11868v1),
  [repo](https://github.com/laude-institute/terminal-bench)): task = instruction + Dockerfile +
  `/tests` + `solution.sh` + time limits (`task.yaml`). Dedicated container per trial +
  **`TmuxSession`** for programmatic terminal control. Three agent adapters: **in-harness Python
  agents**, **installed CLI agents** (Claude Code, Goose as subprocesses), **MCP agent** exposing
  the terminal as MCP tools. Verify: `run-tests.sh` checks **properties of final container state,
  not the agent's commands** → multiple solution paths pass. `FailureMode` enum
  (AGENT_TIMEOUT/TEST_TIMEOUT/PARSING_ERROR). Pass@k.
- **Cybench** ([paper](https://arxiv.org/abs/2408.08926),
  [repo](https://github.com/andyzorigin/cybench)): task = description + starter files + an
  **evaluator holding the secret answer key**; optional **subtasks** for fractional credit.
  **Kali Docker container** + task servers over the network. Agent loop ends each step in an
  **Action** = `Command:` (Bash) or `Answer:` (submit). Verify = compare submitted `Answer:` to
  the key + flag-string matching. Bounded by iteration caps (15 unguided / 5 per subtask).

**SRE takeaways:** (1) give the agent a shell tool into a container (or installed CLI, or MCP
server) — Terminal-Bench's three-adapter design is the model; (2) specify the scenario as data
(fault spec + instruction + rubric/test script); (3) verify by asserting **properties of the
recovered system state** (service healthy, latency restored, error rate at baseline) via scripts
run after the agent finishes — the SRE analog of FAIL_TO_PASS; (4) detect completion via an
explicit "done" signal + a bounded timeout, with an explicit failure-mode enum.

## E. Concrete Go library / tooling recommendations

- **System metrics:** `github.com/shirou/gopsutil/v4` (portable; platform fields in `Ex` structs;
  some Darwin funcs shell out/use cgo). `github.com/prometheus/procfs` for Linux-only minimal
  `/proc`+`/sys` (node_exporter). `github.com/cilium/ebpf` (`bpf2go`) only for tracing-grade needs.
- **Process management:** `os/exec` with `SysProcAttr{Setpgid:true}`, signal the **negative PID**
  to kill the tree (`syscall.Kill(-pid, SIGKILL)`); SIGTERM→SIGKILL for graceful; `exec.CommandContext`
  to bind lifetime. For foreign processes, `gopsutil/v4/process` (`Children()`, `Kill()`, `Terminate()`).
- **Container control:** official Docker client — **path moved: `github.com/docker/docker/client`
  for v28 and earlier; `github.com/moby/moby/client` for v29+** (old module deprecated; recent CVE
  fixes ship only on the new path). `NewClientWithOpts(FromEnv, WithAPIVersionNegotiation())`. Ops:
  `ContainerCreate/Start/Stop/Kill`, `ContainerExec*`, `ContainerStats`/`ContainerStatsOneShot`.
  `github.com/testcontainers/testcontainers-go` (Ryuk auto-cleanup) for integration tests.
- **Terraform:** `github.com/hashicorp/terraform-exec/tfexec` (`Init/Apply/Plan/Destroy/Output`) +
  `terraform-json` + `hc-install`.
- **Structured logging:** stdlib **`log/slog`** (Go 1.21+); back with `rs/zerolog` or `uber-go/zap`
  only if profiling shows logging is hot. Avoid `zerolog.Interface()` with non-primitives in the
  hot path (allocates).
- **CLI:** `github.com/alecthomas/kong` (struct-tag, type-safe, least boilerplate) or `urfave/cli`;
  `spf13/cobra` for many subcommands (optionally `charmbracelet/fang`).
- **Static cross-compilation:** `CGO_ENABLED=0 GOOS=linux GOARCH={amd64,arm64} go build` → fully
  static, scratch/distroless-ready. `go tool dist list` for targets. **Keep the observer CGO-free**
  → `modernc.org/sqlite` (pure-Go) not `mattn/go-sqlite3`. Any cgo dep forces per-target C
  toolchains (Zig cc or `CC=aarch64-linux-gnu-gcc`).

## F. Reference architecture (simulator + observer + scoring)

```
CONTROL PLANE  (Go CLI, kong; static CGO_ENABLED=0)
  scenario spec (YAML/JSON, git-versioned):
    • fault(s): type, target, magnitude, timing
    • infra tier: local-docker | compose | terraform
    • rubric: state assertions defining "recovered" (SRE F2P analog)
        │
   ┌────┴─────┐                 ┌────────────────────────┐
   │ BOOTSTRAP│                 │ AGENT INTERFACE         │
   │ docker/  │                 │ shell tool / installed  │
   │ moby SDK │                 │ CLI / MCP server (T-B   │
   │ + tc;    │                 │ 3-adapter model)        │
   │ terraform│                 └───────────┬────────────┘
   └────┬─────┘                             │ drives recovery
        │                                    │
   ┌────▼────────────────────────────────────▼───────────┐
   │ SYSTEM UNDER TEST (containers / VMs)                 │
   │  FAULT INJECTOR (Go; study Pumba):                   │
   │   stress-ng · tc/netem · cgroups v2 · docker-kill ·  │
   │   Toxiproxy → sidecar shares netns/cgroup            │
   └────┬─────────────────────────────────────────────────┘
        │ degraded host
   ┌────▼─────────────────────────────────────────────────┐
   │ OBSERVER (separate static Go binary, on the host)     │
   │  GOMEMLIMIT+GOGC=off · sync.Pool · fixed pool ·       │
   │  reserved fd · /proc+/sys-first (netlink for sockets) │
   │  USE metrics + dmesg OOM · ▶ fsync'd JSONL (OTel names)│
   └────┬─────────────────────────────────────────────────┘
        │ local files survive net/host degradation
   ┌────▼─────────────────────────────────────────────────┐
   │ SCORING (post-scenario, off hot path)                 │
   │  rotate JSONL → Parquet · load SQLite · DuckDB SQL     │
   │  rubric grader: assert final-state properties →        │
   │  FULL/PARTIAL/NONE + FailureMode enum                  │
   └───────────────────────────────────────────────────────┘
```

**Verification caveats to confirm before building:** JSONL append-atomicity is a property of
write discipline, not the format; OTel system-metric conventions + file-exporter are
Development-stage; the Docker SDK module path moved to `moby/moby` at v29; stress-ng accuracy has
known issues under very high contention (Litmus #3397). Terminal-Bench internal class/method names
came partly from DeepWiki, not line-by-line source reads.

---

<a name="part-5"></a>
# Part 5 — Synthesis: Design Decisions This Research Drove

**The wedge.** No existing benchmark is **local-first / laptop-accessible** *and* scores **real
remediation + MTTR + a genuine safety/blast-radius penalty**, reported with **pass^k reliability +
error bars**. That intersection is both the biggest gap in the prior art and the design most
likely to be credible to the labs whose tables we want to appear in.

**Decisions locked with the user:**

| Decision | Choice | Driven by |
|---|---|---|
| Agent interaction | **Autonomous shell access**; grade transcript + observer-measured recovery | Terminal-Bench/Cybench three-adapter + state-based verification (Part 1C, 4D) |
| Scoring dimensions | **Diagnosis + Remediation/MTTR + Safety/blast-radius + Communication** | Gap analysis: safety + remediation are the open gaps (Part 1E) |
| Scoring math | Per-stage credit (Detect→Diagnose→Mitigate→Resolve); headline **pass^k**, mean ± SE; state-based core; LLM-judge only as labeled secondary | AIOpsLab lifecycle + Cybench subtasks + τ-bench pass^k + Miller error bars (Part 1B–D) |
| Target harness | **Neutral Go loop routed via OpenRouter** (identical loop + tools for every model) | Harness disclosure / "evaluating the harness + model" (Part 1D) |
| Infra tier v1 | **Local Docker first**; Terraform/cloud later | Accessibility wedge; primitives work in plain Docker (Part 4A) |
| First scenario | **OOMKilled / memory exhaustion** | Common + modern + single-container + crisp signal + safety trap (Part 2C, 3D#6) |
| Fault injection | **stress-ng + tc/netem + cgroups + docker-kill** behind a Go iface; study Pumba; Toxiproxy for L4 | Part 4A |
| Observer | **Separate static `CGO_ENABLED=0` Go binary**, `/proc`-first, fsync'd JSONL (OTel names) | Survive-degradation design (Part 4B–C) |
| Result/analysis | fsync'd JSONL hot path → Parquet/SQLite cold; DuckDB for analysis | Part 4C |
| Grade what | **Final system state, not transcript**; oracle-FULL / no-op-ZERO self-test gate | Universal agentic convention + SWE-bench-Verified underspecification lesson (Part 4D, 1C) |

**Things to carry forward into later milestones (from the research, not v1):**
- Deterministic-snapshot tier alongside live-injection (Cloud-OpsBench/ITBench) — for model-card
  tables — reported as a separate column.
- Held-out private split + **canary GUID** + scenario-refresh cadence to resist contamination/saturation.
- Cost/efficiency scoring ($/incident, tool-call efficiency) — only ITBench-AA and AIOpsLab do it.
- Decoy "nothing is wrong" scenarios to test alert-fatigue / acting-when-idle (nobody does this).
- The "monitoring depends on the failed thing" dimension (AWS/Slack/Datadog/Meta) — diagnose-while-blind.
- Native-CLI harness adapters (Claude Code/Codex/Gemini) for "model + its own harness" columns.
- Scale to many scenarios / ≥~1000 instances for tight CIs (v1's single scenario → wide CIs, disclosed).
