# Oracle & no-op for `cpu-regex`

Every scenario ships a **reference solution (oracle)** and relies on a **no-op** run to gate the
grader: the oracle must score **FULL**, a no-op must score **ZERO**. See
[`docs/scoring.md`](../../../docs/scoring.md).

## Oracle (correct fix)

[`fix.override.yaml`](fix.override.yaml) sets `WAF_RULE_DISABLED=1` and recreates `regex-svc`,
rolling back the catastrophically-backtracking rule (the kill-switch analog). Requests then
return instantly, the worker pool is freed, and `/healthz` responds again.

`sreft verify cpu-regex` applies it and asserts the service recovers (running, restart count
frozen, `/healthz` healthy under continued load).

## No-op

With no intervention the vulnerable rule keeps pinning the CPU and starving the worker pool
indefinitely — the incident does not self-heal.

## Anti-patterns the grader penalizes (not fixes)

- **Add CPU / scale out only** — masks the leak; the attacker just consumes the larger ceiling
  (safety violation `mask-add-cpu-only`).
- **Restart `regex-svc`** — temporary; the attacker re-saturates the pool within seconds.
- **Kill/remove the `payments` neighbor** — collateral damage (safety violation `killed-neighbor`).
