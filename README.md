# SRE Field Tests

A reproducible benchmark that scores AI agents/LLMs on realistic **Site Reliability Engineering**
tasks — designed to produce a credible **"SRE score"** that can sit alongside SWE-bench Verified,
GPQA, and the other benchmarks labs cite on model release.

An AI agent is dropped into a live, broken system (an incident), given on-call shell access, and
asked to investigate and restore service. We grade the **recovered system state** — not the
transcript — across four dimensions: **diagnosis, remediation/MTTR, safety/blast-radius, and
communication**, and report **pass^k reliability with real error bars**.

## What makes it different

Unlike existing AIOps/SRE benchmarks (IBM ITBench, Microsoft AIOpsLab, SREGym, RCAEval, …) which
are cloud/Kubernetes-heavy, RCA-focused, and silent on safety, SRE Field Tests is:

- **Local-first / laptop-accessible** — scenarios run in plain Docker (fault primitives are
  cgroups + stress-ng + tc/netem; no cluster). Cloud/Terraform tiers are optional.
- **Remediation-scored** — it grades *fixing* the incident (state-verified, under load), not just
  naming the cause.
- **Safety-scored** — a negative penalty for destructive/unnecessary actions (the biggest open
  gap in prior art).

See [`docs/positioning.md`](docs/positioning.md) and [`RESEARCH.md`](RESEARCH.md) for the full
grounding.

## Vocabulary

- **Scenario** — a git-versioned definition of one SRE activity (e.g. `oom-killed`).
- **Instance** — one run of a scenario against a specific model / harness / seed. Its metadata
  makes each run reproducible and comparable.
- **Infra bootstrap** — the repeatable environment build for a scenario, in **tiers**
  (Tier 0 = local Docker; cloud/Terraform later).

## Architecture

```
sreft (control plane) ──reads──> scenario spec (YAML)
   ├── bootstrap   stand up the infra tier (docker-compose)
   ├── inject      apply the fault (cgroups / stress-ng / tc / docker)
   ├── agentloop   drive any model via OpenRouter through the incident
   ├── observer    separate static binary; survives degradation; writes fsync'd JSONL
   └── score       assert recovered state; per-stage credit; MTTR; safety; aggregate
```

The **observer** is a separate binary on purpose: the process experiencing a fault often can't
reliably monitor at the same time, so a dedicated, resource-hardened observer records results
locally in a crash-safe stream.

## Layout

```
cmd/sreft/          control-plane CLI
cmd/observer/       separate observer binary
internal/           scenario, bootstrap, inject, agentloop, observe, score, instance
scenarios/oom-killed/  the first scenario (README walkthrough, app, bootstrap, oracle, spec.yaml)
docs/               scenario-spec, scoring, result-schema, positioning, walkthrough template
RESEARCH.md         foundational research (benchmarks, SRE, incidents, tooling)
```

## Scenarios

Each scenario ships a **`README.md` walkthrough** — what it is, what a *good* run looks like, and
how the score falls out — alongside its `spec.yaml`. The structure is a project standard, defined
in [`docs/scenario-walkthrough-template.md`](docs/scenario-walkthrough-template.md) and enforced by
a test.

| Scenario | Failure class | Real incidents | Walkthrough |
|---|---|---|---|
| **`oom-killed`** | Memory leak → cgroup OOM (exit 137) crash loop | GKE OOM patterns | [walkthrough](scenarios/oom-killed/README.md) |
| **`cpu-regex`** | Catastrophic regex backtracking (ReDoS) → CPU + worker-pool exhaustion | Cloudflare 2019, Stack Overflow 2016 | [walkthrough](scenarios/cpu-regex/README.md) |

## Build & try

```sh
make build                       # static, CGO-free binaries in ./bin
./bin/sreft --help
./bin/sreft verify oom-killed    # self-test: fault manifests, no-op stays broken, oracle recovers
./bin/sreft run oom-killed --model oracle --harness oracle   # full pipeline, no API key -> scores FULL
./bin/sreft run oom-killed --model noop   --harness noop     # full pipeline, no API key -> scores ZERO
./bin/sreft report               # aggregate graded instances into a scorecard
go test ./...                    # unit tests (Docker self-test is opt-in: SREFT_DOCKER_IT=1)
```

A run bootstraps the stack, starts the separate observer, injects the fault, drives the agent,
observes for the sustain window, grades the recovered state, and tears down. The `oracle`/`noop`
harnesses run the whole thing with **no API key** and are the grader's correctness gate
(oracle → FULL, no-op → ZERO). Live model rows use `--model <openrouter-slug>` and need
`OPENROUTER_API_KEY`.

Status: **the walking skeleton is complete and validated on Docker** — M0 (scaffolding),
M1 (OOM environment + self-test), M2 (neutral agent loop + state-based grader), M3 (aggregation
+ scorecard). The oracle scores 1.00 and the no-op 0.00 on a real observer stream
([docs/scorecard-v0.md](docs/scorecard-v0.md)). The only piece gated on an API key is running
**live models** (issue #5). See the
[Phase 1 milestone](https://github.com/pshima/sre-field-tests/milestone/1); work is tracked in
GitHub Issues.
