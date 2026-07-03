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
scenarios/oom-killed/  the first scenario (app, bootstrap, grader, oracle, spec.yaml)
docs/               scenario-spec, scoring, result-schema, positioning
RESEARCH.md         foundational research (benchmarks, SRE, incidents, tooling)
```

## Build & try

```sh
make build                       # static, CGO-free binaries in ./bin
./bin/sreft --help
./bin/sreft verify oom-killed    # validates the scenario spec
go test ./...
```

Status: **M0 (scaffolding & conventions) complete.** The fault/agent/grader drivers are being
built across milestones M1–M3 — see the [Phase 1 milestone](https://github.com/pshima/sre-field-tests/milestone/1)
and open issues. Work is tracked in GitHub Issues.
