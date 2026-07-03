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

---

## Why this exists

When a lab releases a model, its card carries a row of benchmark scores — coding, math, science,
agentic tasks. There is no credible, comparable **operations** score: *given a production
incident, can this model actually diagnose and fix it — quickly, and without making things worse?*

That question matters as agents move from writing code to running systems, and it's under-served:

- **The prior art is narrow.** IBM ITBench, Microsoft AIOpsLab, SREGym, RCAEval, OpenRCA and
  friends are almost all **cloud/Kubernetes-heavy**, **RCA-focused** (name the cause, don't fix
  it), reuse the same handful of demo apps, and are **silent on safety** — none meaningfully
  penalize destructive remediation or acting when nothing is wrong.
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
- **Harness** — the agent scaffold: `neutral-go` (the OpenRouter tool loop), or the keyless
  reference harnesses `oracle` / `noop`.

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

| Scenario | Failure class | Real incidents | Walkthrough |
|---|---|---|---|
| **`oom-killed`** | Memory leak → cgroup OOM (exit 137) crash loop | GKE OOM patterns | [walkthrough](scenarios/oom-killed/README.md) |
| **`cpu-regex`** | Catastrophic regex backtracking (ReDoS) → CPU + worker-pool exhaustion | Cloudflare 2019, Stack Overflow 2016 | [walkthrough](scenarios/cpu-regex/README.md) |
| **`conn-pool`** | Slow queries hold every pooled DB connection → pool exhaustion (DB idle) | Postgres pool-timeout outages | [walkthrough](scenarios/conn-pool/README.md) |

Both are validated end-to-end on real Docker: oracle → **1.00 FULL**, no-op → **0.00 NONE**.

---

## Repository layout

```
cmd/sreft/            control-plane CLI (up · down · inject · run · score · report · verify)
cmd/observer/         separate observer binary
internal/
  scenario/           spec schema + loader (and the walkthrough-enforcement test)
  bootstrap/          tiered infra (docker-compose today; terraform later)
  inject/             fault drivers (cgroup-oom, cpu-regex)
  agentloop/          OpenRouter tool-use loop; shell/read/write/submit tools; transcript
  observe/            Engine-API collectors + crash-safe JSONL writer/reader
  score/              state-based grader + scorecard aggregation
  selftest/           the `sreft verify` scenario self-test
  refrun/             oracle / no-op reference harnesses (keyless)
  instance/           instance metadata + results layout
scenarios/<id>/       spec.yaml · README.md · app/ · bootstrap/ · oracle/
docs/                 scenario-spec · scoring · result-schema · positioning · walkthrough template · scorecard-v0
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

# Poke at a live environment yourself:
./bin/sreft up oom-killed        # then: docker exec -it sreft-operator bash
./bin/sreft down oom-killed

go test ./...                    # unit tests (the Docker self-test is opt-in: SREFT_DOCKER_IT=1)
```

### Running live models (OpenRouter)

The neutral harness routes any model through OpenRouter's OpenAI-compatible API. Set a key and
run the identical pipeline against real models:

```sh
export OPENROUTER_API_KEY=sk-or-...          # or put it in .env (gitignored)
./bin/sreft run oom-killed --model anthropic/claude-sonnet-5 --seed 1
./bin/sreft run oom-killed --model openai/gpt-5              --seed 1
./bin/sreft run oom-killed --model google/gemini-2.5-pro    --seed 1
# ...≥3 seeds each, then:
./bin/sreft report --out docs/scorecard.md
```

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

**The walking skeleton is complete and validated on Docker**, across two scenarios:

- **M0** scaffolding & data-format conventions · **M1** scenario environment + self-test ·
  **M2** neutral agent loop + state-based grader · **M3** scorecard aggregation.

Next:

- **Live model rows** (issue #5) — the pipeline is ready; it just needs `OPENROUTER_API_KEY`.
- **More scenarios** — connection-pool exhaustion, TLS cert expiry, bad-deploy/rollback, disk-full,
  deadlock, retry storm, … (prioritized in `RESEARCH.md`).
- **Cloud / Terraform Tier-1**, native-CLI harness adapters (Claude Code / Codex / Gemini), a
  deterministic-snapshot tier for model-card tables, a held-out split + canary, and a public
  leaderboard.

Work is tracked in **GitHub Issues**; see the
[Phase 1 milestone](https://github.com/pshima/sre-field-tests/milestone/1).

---

## Documentation

| Doc | What |
|---|---|
| [`RESEARCH.md`](RESEARCH.md) | The foundational research the whole design rests on. |
| [`docs/positioning.md`](docs/positioning.md) | How this compares to existing AIOps/SRE benchmarks, and the wedge. |
| [`docs/scoring.md`](docs/scoring.md) | The scoring engine: dimensions, composite, pass^k, the oracle/no-op gate. |
| [`docs/scenario-spec.md`](docs/scenario-spec.md) | The `spec.yaml` schema. |
| [`docs/scenario-walkthrough-template.md`](docs/scenario-walkthrough-template.md) | The required shape of every scenario walkthrough. |
| [`docs/result-schema.md`](docs/result-schema.md) | The observer stream + instance-directory formats. |
| [`docs/scorecard-v0.md`](docs/scorecard-v0.md) | The first reference scorecard + the wide-CI disclosure. |
