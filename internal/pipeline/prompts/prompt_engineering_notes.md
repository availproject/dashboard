# Prompt Engineering Notes

Lessons from rewriting `team_sync-p2-team_status.instructions.txt`. Apply these when authoring or fixing other pipeline prompts.

---

## 1. State the job before giving rules

The original instructions opened with "When assessing whether a goal is at_risk...". The model had to infer the actual task from the constraints.

**Fix:** open with one sentence stating what the model should produce and why.

```
Assess the likelihood of each business goal and sprint objective being completed
on time, and flag risks that could affect delivery.
```

The rest of the instructions are then clearly in service of that goal.

---

## 2. Principles over decision trees

The original instructions encoded judgment as specific thresholds:
> "A goal with no merged PRs is NOT at risk if sprint_week_progress_pct < 30"

This tries to replace model reasoning with a lookup table. It produces more rules than needed and still misses cases.

**Fix:** express the underlying principle and let the model reason:
> "Calibrate to timing: no progress at the start of a sprint is normal; no progress late in a sprint is a concern."

Use thresholds only when the model genuinely cannot reason without them (e.g. a specific SLA number it has no other way to know).

---

## 3. Don't explain what field names already say

The original instructions defined `days_into_sprint_week`, `sprint_week_progress_pct`, etc. by name. These are self-explanatory.

**Fix:** skip it. If a field name is ambiguous, rename it at the source rather than explaining it in the prompt. Only explain fields whose names are genuinely misleading or whose interpretation requires domain knowledge the model lacks.

---

## 4. Define each output status term explicitly

The original instructions mentioned `likely_done` clearly (rule 7) but `at_risk` and `behind` were only implied by the calibration rules. The model had inconsistent definitions to work from.

**Fix:** give each status value its own definition in parallel form:
- `"likely_done"` requires X
- `"at_risk"` means Y
- `"behind"` is reserved for Z

Make the definitions exclusive so the model isn't guessing which to use.

---

## 5. Require evidence citation, not just correct output

Rules about when to flag risk are hard to enumerate. Instead, requiring the model to cite its evidence in the `note` field forces grounded reasoning and makes wrong assessments easy to spot.

> "Ground every status in specific evidence from the data — open issues, merged PRs, or their absence — and cite it briefly in the 'note' field."

This also makes the output more useful to the user.

---

## 6. Field names in the input shape model behavior

We found the model calling a closed issue "an open bug" because it appeared in a field named `open_issues`. Even with a `"state": "closed"` value in the object, the field name primed the model to treat all items as open.

**Fix:** name data fields by what they contain, not what you expected them to contain. `issues` or `sprint_issues` would have been neutral. If the field name can't be changed, add a one-line note in the instructions: "the issues array contains both open and closed items; check the state field."

---

## 7. Missing data causes false inferences, not silence

When issue #1676 was closed and its label removed, it disappeared from both GitHub search queries. The model then saw it referenced in the sprint plan text but absent from the issues list, and inferred it was still open.

The model will fill gaps with plausible inferences, not say "I don't know." Prompt instructions can't fully fix a data gap — fix it at the data layer (in this case: drop the label filter on closed-issue queries so recently-closed issues stay visible).

---

## 8. Brevity rules belong at the end, briefly

The original had four bullet points about output length. This can be one line:

> "Output: keep 'note' fields to 1-2 sentences. Keep 'sprint_forecast' to 2-3 sentences. Be direct; no filler."

Put output format rules last so they don't crowd the reasoning guidance.
