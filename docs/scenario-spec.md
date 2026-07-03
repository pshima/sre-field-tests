# Scenario spec schema (`scenarios/<id>/spec.yaml`)

A **scenario** is a git-versioned, declarative description of one SRE activity. The control
plane (`sreft`) drives every phase from it; nothing about a scenario is hard-coded in Go beyond
the driver each field selects. The authoritative definition is the Go type in
[`internal/scenario/spec.go`](../internal/scenario/spec.go) (`schema_version: 1`). Loading is
strict — unknown fields are rejected so typos surface immediately.

## Top-level fields

| Field | Type | Purpose |
|---|---|---|
| `schema_version` | int | Spec format version (currently `1`). A newer major version than the binary supports is a hard error. |
| `id` | string | Stable kebab-case ID; must equal the directory name. |
| `title`, `summary` | string | Human description. |
| `category` | string | Failure class (Gunawi-2016-style): `resource-exhaustion`, `bad-change`, `network`, `database`, … |
| `difficulty` | string | `easy` \| `medium` \| `hard`. |
| `human_resolve_minutes` | int | Est. time a competent SRE needs; calibrates difficulty + time scoring. |
| `references[]` | list | Real post-mortems/incidents the scenario models (`title`, `url`, `note`). |
| `fault` | object | The injected failure (see below). |
| `tiers{}` | map | Infra-tier name → bootstrap. v1 ships `tier0-docker`. |
| `task` | object | What the agent is told and how it connects. |
| `observer` | object | What the observer captures. |
| `rubric` | object | How a run is graded. |

## `fault`

| Field | Purpose |
|---|---|
| `kind` | Injector driver: `cgroup-oom`, `cpu-hog`, `net-latency`, `process-kill`, `toxiproxy`, … (registry in `internal/inject`). |
| `target` | SUT component (a compose service). |
| `params{}` | Driver-specific settings (kept open so new fault classes need no schema change). |
| `start_delay_seconds` | Warm-up before the fault activates. |

## `tiers{}` → `InfraTier`

| Field | Purpose |
|---|---|
| `kind` | Bootstrap driver: `docker-compose` (v1) or `terraform` (later). |
| `path` | Tier dir relative to the scenario root. |
| `cost` | Informational: `free-local`, `cloud-cheap`, … |

## `task`

| Field | Purpose |
|---|---|
| `prompt` | The incident "page" handed to the agent. |
| `operator_service` | Compose service the agent gets a shell in (on-call-operator access, e.g. docker socket). |
| `max_iterations`, `wall_clock_seconds` | Budgets (0 = harness default). |

## `observer`

| Field | Purpose |
|---|---|
| `collectors[]` | Enabled collectors: `cgroup-mem`, `docker-events`, `http-health`, `proc-fd`, … |
| `interval_ms` | Sampling period. |
| `targets{}` | Logical name → compose service / health URL. |

## `rubric`

Core scoring is **state-based** (assert the recovered system), with the LLM judge only a
labeled secondary metric. See [scoring.md](scoring.md).

| Field | Purpose |
|---|---|
| `weights` | Composite weights: `diagnosis`, `remediation`, `communication` (sum to 1.0) + `safety_penalty` scale. |
| `stages[]` | Lifecycle stages for partial credit: `detect`, `diagnose`, `mitigate`, `resolve` (each `weight` + `description`). |
| `health_check` | State assertion for "recovered": `url`, `sustain_seconds`, `under_load`. |
| `root_cause_key[]` | Answer-key keywords/phrases the agent's RCA must cover. |
| `safety_violations[]` | Detectable destructive actions: `id`, `description`, `penalty`, `detector` (`sentinel-file` \| `container-gone` \| `command-audit`), `params`. |

See [`scenarios/oom-killed/spec.yaml`](../scenarios/oom-killed/spec.yaml) for a complete worked example.

## `oracle`

The reference-solution answer, used by the `oracle` harness (the grader's FULL correctness gate).
The fix itself is the compose override at `oracle/fix.override.yaml`; this is the matching
diagnosis a correct responder would submit.

| Field | Purpose |
|---|---|
| `oracle.submission.root_cause` | The correct root cause (should cover the `rubric.root_cause_key`). |
| `oracle.submission.actions` | What the reference fix did. |
| `oracle.submission.postmortem` | A short blameless postmortem. |

## Companion walkthrough (required)

`spec.yaml` is the machine-readable definition. Every scenario **must** also ship a human-facing
`README.md` walkthrough next to it — what the scenario is, what a good run looks like, and how it
is scored — following [`scenario-walkthrough-template.md`](scenario-walkthrough-template.md). This
is enforced by a test in `internal/scenario`.
