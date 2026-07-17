# SRE Field Tests

**A reproducible benchmark that scores AI agents/LLMs on realistic Site Reliability Engineering
work** — designed to produce a credible **"SRE score"** that can sit alongside SWE-bench Verified,
GPQA, and the other benchmarks AI labs cite when they ship a model.

An agent is dropped into a live, broken system — a real incident reproduced in containers — given
an on-call operator's shell, and asked to investigate and restore service. A separate observer
records what actually happens to the system, and a grader scores the **recovered state** (not the
agent's transcript) across four dimensions: **diagnosis, remediation & MTTR, safety / blast-radius,
and communication** — reported with **pass^k reliability and real error bars**.

Everything runs on a laptop in plain Docker. No cluster, no cloud account, no API key required to
try it (the `oracle` and `noop` reference harnesses exercise the whole pipeline offline).

> **📊 First results — [interactive scorecard v0](https://pshima.github.io/sre-field-tests/)**
> (8 agents × 6 scenarios × 108 instances). Source + per-instance records in
> [`benchmark-results/scorecard-v0/`](benchmark-results/scorecard-v0/).

---

## Why this exists

When a lab releases a model, its card carries a row of benchmark scores — coding, math, science,
agentic tasks. There is no credible, comparable **operations** score: *given a production
incident, can this model actually diagnose and fix it — quickly, and without making things worse?*

That question matters as agents move from writing code to running systems, and it's under-served:

- **The prior art is narrow.** IBM ITBench, Microsoft AIOpsLab, SREGym, RCAEval, OpenRCA and
  friends are almost all **cloud/Kubernetes-heavy** and stop at **naming the cause** over a frozen
  snapshot — the system isn't running, so nothing is actually fixed. We drop the agent into a
  **live** broken stack and grade the **restored state** (the failing check now passes and *stays*
  passing under load). They also reuse the same handful of demo apps and are **silent on safety** —
  none meaningfully penalize destructive remediation, masking-not-fixing, or **acting when nothing
  is wrong** (a live system is what makes those scorable at all).
- **Benchmarks are gamed by omission.** Most report a single pass@1 with no error bars, no
  reliability metric, and an undisclosed harness — and a scaffold can move an agentic score 10–20
  points without changing the model.

SRE Field Tests targets the gap no one occupies: **local-first**, **remediation- and
safety-scored**, **reliability-reported**, with a **fully disclosed neutral harness**. See
[`docs/positioning.md`](docs/positioning.md) and [`RESEARCH.md`](RESEARCH.md) for the full grounding
(a four-pillar research pass covering benchmark methodology, SRE practice, real incident
post-mortems, and fault-injection tooling — every design decision traces back to it).

---

## How a run works

One **instance** = one run of one **scenario** against one model/harness/seed. Running it drives
this pipeline end-to-end and leaves a self-contained, auditable results directory:

```
  bootstrap ──▶ start observer ──▶ inject fault ──▶ run agent ──▶ observe sustain ──▶ grade ──▶ teardown
   (docker      (separate           (arm the         (the model     window          (state-      (clean
    compose)     process,            failure)         works the      (does the        based        slate)
                 crash-safe                           incident)      fix hold?)        grader)
                 JSONL)
```

1. **Bootstrap** — `docker compose up` stands up the scenario's Tier-0 stack: the vulnerable
   service, a load/attacker driver, an unrelated **neighbor** service (a safety trap), and an
   **operator** container the agent gets a shell in (docker CLI + `curl`, `ps`, …).
2. **Observer** — a *separate* static binary starts recording the system's timeline (memory, CPU,
   container events, health) to an append-only, fsync'd `observer.jsonl`. It's a distinct process
   on purpose: whatever is experiencing the fault often can't reliably monitor itself.
3. **Inject** — the fault is armed (for the current scenarios the failure mechanism is declared in
   the compose stack, and the injector honors a warm-up delay); the moment it activates is the
   zero point for MTTR.
4. **Agent** — the harness drives the model through the incident. The neutral harness gives every
   model the *identical* tool loop (`shell`, `read_file`, `write_file`, `submit`) so the score
   reflects the model, not a bespoke wrapper. It records the full transcript.
5. **Observe the sustain window** — after the agent finishes, the observer keeps recording for the
   scenario's sustain window (e.g. 60 s) so the grader can tell a *durable* fix from one that
   looks good for a moment and then regresses — under continued load.
6. **Grade** — the grader reads the observer stream + the agent's submission and scores the
   **recovered state** (below). Never the transcript.
7. **Teardown** — the stack is removed; the results directory (`meta.json`, `observer.jsonl`,
   `transcript.jsonl`, `submission.json`, `score.json`) remains for audit.

---

## How scoring works

Scoring is **state-based** — the load-bearing principle. After the agent is done, the grader
asserts properties of the *system* (is the service healthy and sustained under load? did the
crash loop stop? was a neighbor harmed?), the SRE analog of SWE-bench's `FAIL_TO_PASS` /
`PASS_TO_PASS`. Four dimensions, each 0–1, weighted per scenario:

| Dimension | What it measures |
|---|---|
| **Diagnosis** | Did the submitted RCA name the real root cause? (answer key, token-subset match, folded with a "detect" stage) |
| **Remediation** | Is the service **healthy and sustained under load** in the observer stream? Plus **MTTR** from the timestamps. |
| **Safety** | A **negative** penalty for destructive / unnecessary / risky actions (deleting data, killing a neighbor, masking instead of fixing). |
| **Communication** | Postmortem quality via LLM-judge — a **labeled secondary** metric, skipped when no judge is configured. |

Partial credit accrues across a **Detect → Diagnose → Mitigate → Resolve** lifecycle. The
composite is the weighted sum of the scored positive dimensions minus the safety penalty; a run is
`FULL` only if it resolved the incident, diagnosed it well, **and** stayed safe. Across seeds the
scorecard reports **mean ± SE** and **pass^k** (the probability *all* k seeds resolve — because an
agent that fixes an incident 1-in-4 times is dangerous).

Every scenario ships a reference **oracle** (the known-good fix) and relies on a **no-op** run as
the grader's correctness gate: **oracle must score `FULL`, no-op must score `ZERO`**. This is the
guard that a scenario matches its description and that the grader is neither too lenient nor too
strict. Full mechanics: [`docs/scoring.md`](docs/scoring.md); a worked example per scenario lives
in each scenario's walkthrough.

---

## Design principles

- **Grade final state, not the transcript** — the universal agentic-benchmark convention.
- **Local-first / accessible** — faults are built on Linux primitives (cgroups today; `stress-ng`,
  `tc/netem`, Toxiproxy are the roadmap building blocks) in plain Docker, so anyone can run it.
  Cloud/Terraform tiers layer on later behind the same interface.
- **A separate, resource-hardened observer** — a static `CGO_ENABLED=0` binary that writes a
  crash-safe local stream, so results survive host/network degradation.
- **A neutral, fully-disclosed harness** — one identical loop for every model (routed via
  OpenRouter), the scaffold and prompts published, transcripts retained (HELM-style).
- **Reliability over single-shot** — pass^k and error bars, following the eval-error-bars guidance.
- **Reproducibility** — scenarios are git-versioned data; instances pin model, seed, sampling, and
  timestamps; the oracle/no-op gate runs in CI.

---

## Vocabulary

- **Scenario** — a git-versioned definition of one SRE activity (e.g. `oom-killed`), consisting of
  a `spec.yaml`, the system-under-test app, its bootstrap, an oracle fix, and a README walkthrough.
- **Instance** — one run of a scenario against a specific model / harness / seed. Its metadata make
  each run reproducible and comparable (`sonnet-5 × oom-killed × seed 1` is one instance).
- **Infra bootstrap** — the repeatable environment build, in **tiers**: Tier 0 = local Docker
  (built); cloud/Terraform = later.
- **Harness** — the agent scaffold: `neutral-go` (the OpenRouter tool loop), `claude-cli` /
  `codex-cli` (installed CLI agents driven headlessly, using their subscriptions), or the keyless
  reference harnesses `oracle` / `noop`. Each is a separate, disclosed scorecard column.

---

## Architecture

```
sreft (control-plane CLI, kong) ── reads ──▶ scenario spec (YAML)
  │
  ├── bootstrap   stand up the infra tier            (internal/bootstrap · docker-compose)
  ├── inject      arm the fault                       (internal/inject · cgroup-oom, cpu-regex)
  ├── agentloop   drive any model through the incident(internal/agentloop · OpenRouter tool loop)
  ├── observer    separate binary; crash-safe JSONL   (cmd/observer · cgroup-mem/-cpu, events, http)
  └── score       assert recovered state; aggregate   (internal/score · grader + scorecard)
```

All binaries are static and CGO-free. The observer reads the Docker Engine API over the unix
socket using only the standard library — no heavy SDK — to stay a small, robust degradation
survivor. Result streams are fsync'd JSON Lines with OpenTelemetry-style field names; DuckDB can
query them directly for analysis.

---

## Scenarios

Each scenario ships a **`README.md` walkthrough** — what it is, what a *good* run looks like, and
how the score falls out — alongside its `spec.yaml`. The structure is a project standard, defined
in [`docs/scenario-walkthrough-template.md`](docs/scenario-walkthrough-template.md) and **enforced
by a test** (a scenario can't merge without its walkthrough).

**Six scenarios** across four failure classes:

| Scenario | Failure class | Real incidents | Walkthrough |
|---|---|---|---|
| **`oom-killed`** | *resource-exhaustion* — memory leak → cgroup OOM (exit 137) crash loop | GKE OOM patterns | [walkthrough](scenarios/oom-killed/README.md) |
| **`cpu-regex`** | *resource-exhaustion* — catastrophic regex backtracking (ReDoS) → CPU + worker-pool exhaustion | Cloudflare 2019, Stack Overflow 2016 | [walkthrough](scenarios/cpu-regex/README.md) |
| **`conn-pool`** | *resource-exhaustion* — slow queries hold every pooled DB connection → pool exhaustion (DB idle) | Postgres pool-timeout outages | [walkthrough](scenarios/conn-pool/README.md) |
| **`bad-deploy`** | *bad-change* — a broken release (v2) fails health; the fix is to roll back, not tune | Change-to-a-live-system (~70% of outages), Knight Capital 2012 | [walkthrough](scenarios/bad-deploy/README.md) |
| **`retry-storm`** | *dependency* — unbounded retries against a degraded dependency saturate the caller's pool | AWS DynamoDB retry storm 2015 | [walkthrough](scenarios/retry-storm/README.md) |
| **`false-alarm`** | *abstention* — a page fires but nothing is broken; the correct move is to **change nothing** | Noisy alerting / on-call reflexes | [walkthrough](scenarios/false-alarm/README.md) |

Every scenario is validated end-to-end on real Docker: oracle → **1.00 FULL**, no-op → **0.00 NONE**
(`false-alarm` inverts the gate — the correct reference *abstains* and scores FULL). See the first
cross-model results in the [live scorecard](https://pshima.github.io/sre-field-tests/).

---

## Repository layout

```
cmd/sreft/            control-plane CLI (up · down · inject · run · bench · rescore · score · report · verify)
cmd/observer/         separate observer binary
internal/
  scenario/           spec schema + loader (and the walkthrough-enforcement test)
  suite/              benchmark suite schema + run manifest
  bootstrap/          tiered infra (docker-compose today; terraform later)
  inject/             fault drivers (cgroup-oom, cpu-regex, conn-pool, bad-deploy, retry-storm, none)
  agentloop/          OpenRouter tool-use loop; shell/read/write/submit tools; prompt caching; token/$ usage; transcript
  cliagent/           installed-CLI harness adapters (claude-cli, codex-cli)
  observe/            Engine-API collectors + crash-safe JSONL writer/reader
  score/              state-based grader (incl. no-change/abstention mode) + scorecard aggregation (tokens/$)
  selftest/           the `sreft verify` scenario self-test
  refrun/             keyless reference harnesses: oracle · noop · abstain · always-restart · mask
  instance/           instance metadata (+ token/$ usage) + results layout
scenarios/<id>/       spec.yaml · README.md · app/ · bootstrap/ · oracle/  (+ baselines/ where a mask baseline ships)
suites/               benchmark suites: smoke · cli-sweep · subscription · openrouter · baselines · baselines-mask
runs/                 bench output: runs/<id>/ with manifest.json + instances + scorecard.md (gitignored)
benchmark-results/    committed, published results (scorecard-v0/: SCORECARD.md, scorecard.html, per-instance records)
docs/                 scenario-spec · scoring · running-benchmarks · reproducing · baselines · result-schema · positioning · walkthrough template
RESEARCH.md           foundational research (benchmarks, SRE, incidents, tooling)
```

---

## Quickstart

```sh
make build                                              # static, CGO-free binaries in ./bin

# Prove a scenario matches its description (bootstrap → fault manifests → no-op
# stays broken → oracle recovers → teardown). No API key needed.
./bin/sreft verify oom-killed
./bin/sreft verify cpu-regex

# Run the full pipeline with a keyless reference harness (the grader's gate):
./bin/sreft run oom-killed --model oracle --harness oracle   # → 1.00 FULL
./bin/sreft run oom-killed --model noop   --harness noop     # → 0.00 NONE

# Aggregate all graded instances into a scorecard:
./bin/sreft report

# Run a whole benchmark suite (matrix of scenarios × harnesses × seeds),
# strictly one cell at a time, into a self-contained run directory:
./bin/sreft bench suites/smoke.yaml            # keyless pipeline smoke test
./bin/sreft bench suites/cli-sweep.yaml        # claude-cli + codex-cli × all scenarios
./bin/sreft report  --run <run-id>             # scorecard for a run
./bin/sreft rescore <run-id>                   # re-grade a run from saved artifacts (no re-run)

# Poke at a live environment yourself:
./bin/sreft up oom-killed        # then: docker exec -it sreft-operator bash
./bin/sreft down oom-killed

go test ./...                    # unit tests (the Docker self-test is opt-in: SREFT_DOCKER_IT=1)
```

A **suite** ([`suites/`](suites)) declares the matrix; a **run** (`runs/<id>/`) is a reproducible
artifact with a manifest + scorecard. See [`docs/running-benchmarks.md`](docs/running-benchmarks.md).

### Running real agents

A **harness** is the agent scaffold that works the incident. The same pipeline (observe → grade)
scores every harness; they differ only in *who* drives the shell, so each is a distinct,
disclosed scorecard column.

**Installed CLI agents (no API key — uses your existing subscriptions).** `claude-cli` and
`codex-cli` run the installed Claude Code / Codex CLI headlessly on the host with docker access to
the scenario stack, and translate the CLI's transcript into the standard instance artifacts:

```sh
./bin/sreft run oom-killed --harness claude-cli --model default --seed 1
./bin/sreft run oom-killed --harness codex-cli  --model default --seed 1
./bin/sreft report
```

These operate from an "on-call from my laptop" vantage (host shell + docker), which is distinct
from the neutral harness's in-container operator shell — expected, and recorded in the harness
column. They run autonomously (`--permission-mode bypassPermissions` / Codex's bypass flag), so
run them only against a scenario you've stood up.

**Neutral harness (OpenRouter).** `neutral-go` routes any model through OpenRouter's
OpenAI-compatible API — every model runs the *identical* Go tool loop, the fairest cross-model
comparison. It applies prompt caching and records per-run token + `$` cost. Needs a key, which
`sreft` auto-loads at startup (in order: an already-set `OPENROUTER_API_KEY`, a `KEY=VALUE` line in
`.env`, or a raw `OPENROUTER_KEY` file — all gitignored):

```sh
echo 'sk-or-...' > OPENROUTER_KEY             # or: export OPENROUTER_API_KEY=... / put it in .env
./bin/sreft run oom-killed --harness neutral-go --model anthropic/claude-sonnet-5 --seed 1
# ...≥2–3 seeds each, then:
./bin/sreft report                            # scorecard incl. Tokens and $/inc columns
```

The first cross-model run — **8 agents × 6 scenarios × 108 instances** — is published as the
[live scorecard](https://pshima.github.io/sre-field-tests/) with source under
[`benchmark-results/scorecard-v0/`](benchmark-results/scorecard-v0/).

---

## Adding a scenario

Scenarios are self-contained directories. To add one:

1. `scenarios/<id>/spec.yaml` — fault, infra tier, agent task, observer config, rubric (weights,
   stages, answer key, safety detectors), and the `oracle.submission` reference answer. See
   [`docs/scenario-spec.md`](docs/scenario-spec.md).
2. `scenarios/<id>/app/` — the system-under-test (its own tiny module) + `Dockerfile`.
3. `scenarios/<id>/bootstrap/tier0/docker-compose.yaml` — the SUT, a load driver, a neighbor, an
   operator shell.
4. `scenarios/<id>/oracle/fix.override.yaml` — the known-good fix (a compose override).
5. `scenarios/<id>/README.md` — the walkthrough, following the standard (a **test fails** without it).
6. Register a fault-kind injector in `internal/inject` if the failure mechanism is new, and add an
   observer collector in `internal/observe` if it needs a new signal.
7. `sreft verify <id>` — confirm the fault manifests, a no-op stays broken, and the oracle recovers.

---

## Status & roadmap

**The pipeline is complete, validated, and has produced its first real scorecard** — the full
skeleton (M0 scaffolding · M1 scenario environment + self-test · M2 neutral agent loop + state-based
grader · M3 scorecard aggregation) plus:

- **Six scenarios** across four failure classes (resource-exhaustion, bad-change, dependency,
  abstention), each with an oracle→FULL / no-op→ZERO gate and deterministic non-LLM reflex
  baselines to prove non-triviality.
- **First cross-model scorecard** — 8 agents (`claude-cli`, `codex-cli`, and six OpenRouter
  flagships) × 6 scenarios × 108 instances, with pass^k reliability and real token/`$` cost.
  Published as a [live scorecard](https://pshima.github.io/sre-field-tests/); source under
  [`benchmark-results/scorecard-v0/`](benchmark-results/scorecard-v0/).
- **Neutral OpenRouter rows** (issue #5) — done: prompt-cached, cost-accounted runs across the
  frontier models.

Next:

- **More scenarios** — TLS cert expiry, disk-full, deadlock, cache stampede, … (prioritized in
  `RESEARCH.md`); grow toward the ~1000-instance scale that tightens the confidence intervals.
- **Cloud / Terraform Tier-1**, a Gemini CLI adapter, a deterministic-snapshot tier for model-card
  tables, a held-out split + canary, and a public leaderboard.

Work is tracked in **GitHub Issues**; see the
[Phase 1 milestone](https://github.com/pshima/sre-field-tests/milestone/1).

---

## Documentation

| Doc | What |
|---|---|
| [`RESEARCH.md`](RESEARCH.md) | The foundational research the whole design rests on. |
| [`docs/positioning.md`](docs/positioning.md) | How this compares to existing AIOps/SRE benchmarks, and the wedge. |
| [`docs/running-benchmarks.md`](docs/running-benchmarks.md) | Suites, `sreft bench`/`rescore`/`report`, run directories, and cost accounting. |
| [`docs/scoring.md`](docs/scoring.md) | The scoring engine: dimensions, composite, pass^k, the oracle/no-op gate, abstention. |
| [`docs/baselines.md`](docs/baselines.md) | The deterministic non-LLM reflex baselines and the non-triviality proof. |
| [`docs/reproducing.md`](docs/reproducing.md) | Cost/token accounting and keyless re-scoring (`sreft rescore`). |
| [`docs/scenario-spec.md`](docs/scenario-spec.md) | The `spec.yaml` schema. |
| [`docs/scenario-walkthrough-template.md`](docs/scenario-walkthrough-template.md) | The required shape of every scenario walkthrough. |
| [`docs/result-schema.md`](docs/result-schema.md) | The observer stream + instance-directory formats. |
| [`docs/scorecard-v0.md`](docs/scorecard-v0.md) | The first cross-model scorecard (pointer to the published results). |
| [live scorecard](https://pshima.github.io/sre-field-tests/) | The published v0 results — 8 agents × 6 scenarios, interactive. |
