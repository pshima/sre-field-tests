# Reproducing results & the cost frontier

Results are auditable and re-gradeable without an API key, and every real-model run records what it
cost so the scorecard can show a **cost-vs-quality frontier**, not just a quality score.

## Cost accounting (tokens + $)

Each instance records the resource cost of the run in its `meta.json`:

```json
"usage": { "prompt_tokens": 41200, "completion_tokens": 3800, "total_tokens": 45000, "cost_usd": 0.061 }
```

- The **neutral-go** harness opts into OpenRouter's usage accounting (`usage.include`) and
  accumulates per-turn `prompt_tokens` / `completion_tokens` / `total_tokens` and the run's dollar
  `cost` across the whole loop (`internal/agentloop/loop.go`).
- The scorecard rolls this up per row into **Tokens** (mean total tokens/instance) and **$/inc**
  (mean $/instance). Rows from harnesses that report no usage — the keyless `oracle` / `noop` /
  reflex baselines, and the CLI adapters until they surface native usage — show `—` rather than a
  misleading `$0`, and are excluded from the cost means.

```sh
sreft report                 # scorecard incl. Tokens and $/inc columns
sreft report --format json   # the same, machine-readable (tokens_mean, cost_usd_mean)
```

## Keyless re-scoring

Every instance directory is self-contained (`meta.json`, `transcript.jsonl`, `observer.jsonl`,
`submission.json`, `score.json`). The grader is state-based, so a completed run can be **re-graded
with no API key and no re-run of the agent** — useful for auditing a scorecard, or re-scoring after
a rubric change:

```sh
sreft rescore <run-dir>      # re-grade a whole run from its saved artifacts
sreft score  <instance-dir>  # re-grade a single instance
```

The reference and reflex baselines (`oracle`, `noop`, `always-restart`, `mask`) reproduce their
scores fully offline — they need no key at all:

```sh
sreft bench suites/baselines.yaml && sreft report --run <id>
```

## Follow-ups

- **CLI-native usage.** The `claude-cli` / `codex-cli` adapters do not yet parse the token/cost
  fields in their native event streams, so their rows show `—`. Wiring that up is a follow-up.
- **Published real-model trajectories.** Committing a frozen real-model run (transcripts + scores +
  usage) for keyless re-scoring depends on the first keyed runs — see issue #5
  (`OPENROUTER_API_KEY`). The mechanism (`sreft rescore`) is already in place.
