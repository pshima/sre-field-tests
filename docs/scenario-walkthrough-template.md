# Scenario walkthrough — the standard

**Every scenario ships a `README.md`** next to its `spec.yaml`, following the structure below.
It is the human-facing companion to the machine-readable spec: it explains what the scenario is,
what a *good* run looks like, and how the score falls out — the doc a person reads to understand
the scenario without reading Go.

This is enforced: a test (`internal/scenario`) fails if any `scenarios/<id>/` lacks a `README.md`
or `spec.yaml`. The canonical example is [`scenarios/oom-killed/README.md`](../scenarios/oom-killed/README.md);
copy it and adapt.

## Required sections

1. **TL;DR** — one blockquote: the fault in a sentence, plus difficulty, category, human
   resolve time, tier, and the real incident(s) it models.
2. **The setup** — a table of the environment components and each one's role, including the
   *trap* components (stable neighbors, sentinels) and the operator shell.
3. **The failure** — how the fault unfolds under load, and the concrete **signals an SRE reads**
   to spot it (exit codes, log lines, metrics, `/proc` or cgroup fields).
4. **The page** — the exact prompt the agent receives (from `spec.yaml` `task.prompt`). No
   hints beyond what a real page would contain.
5. **What "good" looks like** — a Detect → Diagnose → Mitigate → Resolve table describing the
   ideal trajectory, plus the safety expectations.
6. **The traps** — the plausible-but-wrong actions the scenario is designed to separate from a
   real fix (masking, symptom-only mitigation, collateral damage), and the one correct fix.
7. **How it's scored** — the four dimensions with their weights (from `spec.yaml`), the state
   assertions behind remediation, the safety detectors and penalties, the MTTR definition, the
   **composite formula**, the verdict rule, and a **worked-examples table** using the real
   formula (ideal → partial variants → no-op).
8. **Verify it yourself** — the `sreft verify` / `run --harness oracle|noop` / `report` commands.
9. **References** — the real post-mortems / docs the scenario is grounded in.

## Guidance

- **Keep the worked-examples table honest.** Compute each row with the scenario's real weights
  and stage credits, the way the grader does. The middle rows (partial credit, safety penalties)
  are the most valuable — they show what the scenario actually discriminates.
- **State the fix and the traps explicitly.** A scenario is only as good as its ability to
  separate a real fix from a plausible non-fix; the walkthrough should make that separation
  obvious.
- **Every claim about scoring must match `spec.yaml` and the grader.** If you change the rubric,
  update the walkthrough in the same commit.
- **Point to, don't duplicate, the generic engine.** Scoring mechanics common to all scenarios
  live in [`scoring.md`](scoring.md); the walkthrough covers what is *specific* to this scenario.
