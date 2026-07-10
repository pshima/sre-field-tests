# Result data format

Two on-disk artifacts carry a run's results. Both are chosen so results survive a degraded host
and stay auditable long after the run.

## Observer stream — `observer.jsonl`

Append-only, newline-delimited JSON, written with `O_APPEND` and fsync'd (one complete record per
write). Append crash-safety is a property of that *write discipline*, not of JSON — a torn
trailing line is silently dropped on read; a corrupt interior line is a hard error (real data
loss must not be hidden). See [`internal/observe`](../internal/observe). Field names follow
**OpenTelemetry semantic conventions** where one exists, so the stream speaks a standard
vocabulary even though the container is plain JSONL.

Each line is one `Record`, either a **sample** (a metric reading) or an **event** (a discrete
occurrence). Keeping both in one ordered stream preserves the exact ordering between "memory hit
the limit" and "OOM kill fired" — the crux of grading a resource-exhaustion scenario.

```json
{"ts":"2026-07-02T18:00:00.123Z","kind":"sample","collector":"cgroup-mem","target":"orders","metric":"system.memory.usage","value":268435456,"unit":"By"}
{"ts":"2026-07-02T18:00:00.130Z","kind":"event","collector":"docker-events","target":"orders","event":"oom_kill","attrs":{"exit_code":137,"oom_kill_total":3}}
```

Common metric names and event types are centralized as constants in
[`internal/observe/record.go`](../internal/observe/record.go) so the observer and grader never
disagree on a string: `system.memory.usage`, `system.memory.limit`, `system.memory.oom_kill.count`,
`system.cpu.utilization`, `process.memory.rss`, `process.open_file_descriptors`,
`http.server.request.duration`, `service.health.up`, `container.restart.count`; events `oom_kill`,
`container_restart`, `container_exit`, `container_health`. Collectors (enabled per scenario):
`cgroup-mem`, `cgroup-cpu`, `docker-events`, `http-health`, `proc-fd`.

**Cold-path analysis.** JSONL is the hot-path format. For analysis, rotate it into immutable
Parquet snapshots or load into SQLite — or just point **DuckDB** at the JSONL directly (it reads
JSONL, Parquet, and SQLite uniformly via SQL), so the storage choice never locks the analysis
choice and conversion can be deferred.

## Instance directory — one per run

Self-contained so results remain auditable (HELM-style). See
[`internal/instance`](../internal/instance/instance.go).

```
<results-dir>/<instance-id>/
  meta.json          Metadata: scenario, model, harness, seed, sampling, timestamps, git-sha,
                     and usage (prompt/completion/total tokens + cost_usd) when the harness reports it
  transcript.jsonl   the agent's tool calls (shell commands), normalized across harnesses;
                     this is what the safety command-audit scans
  messages.jsonl     the full agent conversation / raw CLI event stream (audit only)
  observer.jsonl     the observer stream above
  submission.json    the agent's final RCA / postmortem (root_cause, actions_taken, postmortem)
  score.json         the grader's per-dimension Result
  codex-last.txt     codex-cli only: the CLI's final message (submission fallback source)
```

Every harness (neutral-go, claude-cli, codex-cli, oracle, noop) writes the same
`transcript.jsonl` / `submission.json` shape, so the grader is harness-agnostic. The CLI
adapters translate their native output into these files (each tool/`Bash` call is normalized to a
`shell`/`cmd` tool call).

`instance-id` = `<scenario>__<model>__seed<n>__<UTC timestamp>`, e.g.
`oom-killed__anthropic-claude-sonnet-5__seed1__20260703T011205Z`.
