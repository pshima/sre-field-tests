# Baselines — the non-triviality proof

A benchmark whose incidents fall to a one-line reflex measures nothing. Alongside the two grader
gates every scenario already ships — **oracle → FULL**, **no-op → ZERO** — we run deterministic,
keyless **reflex baselines** through the *same* grader. A credible scenario must defeat them: a
reflex that scores **FULL** is a scenario bug, not a passing agent.

These are policies, not agents — no LLM, no API key. They implement `agentloop.Runner` in
[`internal/refrun`](../internal/refrun/refrun.go) and are selectable as harnesses.

| Harness | Policy | Expected verdict |
|---|---|---|
| `oracle` | Apply the scenario's known-good fix (`oracle/fix.override.yaml`) + submit the correct RCA. | **FULL** |
| `noop` | Do nothing. The incident does not self-heal. | **NONE** |
| `always-restart` | Bounce the affected service (`docker compose restart`) and submit a generic "I restarted it." | **PARTIAL / NONE** |
| `mask` | Apply the scenario's `baselines/mask.override.yaml` — raise the limit / enlarge the pool / add workers — without addressing the cause. | **PARTIAL** (never FULL) |

## Why each reflex fails

- **`always-restart`** — our scenarios are built so a restart never *durably* fixes the fault: the
  leak leaks again, the pool re-exhausts, the bad release is still bad, the retry storm resumes.
  Health does not stay up through the sustain window, so remediation earns ~0.
- **`mask`** — where masking transiently restores health it earns *remediation* credit but names no
  root cause, so **diagnosis ≈ 0** and it cannot reach FULL (FULL requires diagnosis ≥ 0.8). This is
  the sharpest signal the benchmark produces: *fixing the symptom without understanding it scores
  differently from fixing the cause.* A real agent doing the same trips the scenario's `mask-*`
  safety violation on top.

## Observed (reference harnesses, keyless)

| Scenario | Harness | Verdict | Composite | Diag | Remed |
|---|---|---|--:|--:|--:|
| oom-killed | always-restart | partial | 0.13 | 0.30 | 0.00 |
| conn-pool | mask | partial | 0.56 | 0.00 | 1.00 |
| bad-deploy | always-restart | partial | 0.13 | 0.30 | 0.00 |

No reflex reaches FULL on any scenario.

## Running them

```sh
# All scenarios × {oracle, noop, always-restart} — the universal non-triviality sweep:
sreft bench suites/baselines.yaml && sreft report --run <id>

# The masking reflex on the resource-exhaustion scenarios that ship a mask override:
sreft bench suites/baselines-mask.yaml && sreft report --run <id>

# Or a single reflex, ad hoc:
sreft run conn-pool --model reflex --harness mask
sreft run oom-killed --model reflex --harness always-restart
```

The `mask` harness requires the scenario to ship `scenarios/<id>/baselines/mask.override.yaml`
(the resource-exhaustion scenarios do); it errors clearly otherwise.
