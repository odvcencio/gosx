# Ouroboros O0.2 governance policy

`compatibility_receipt.v1.json` and `corpus.v1.json` are immutable historical
evidence. Their source revision, measured runtime sizes, and receipt counts are
not rewritten to make a later candidate pass. Planned runtime variants remain
unmeasured (`null`) until a reproducible collector produces real artifacts.

The historical source and ambient-surface counts are ceilings rather than
equality pins. A reduction is allowed; growth fails closed. Candidate-to-
baseline comparisons take the corresponding monotonic rules from
`budgets.v1.json`. Ambient names are also compared as sets, so replacing one
name with another cannot evade the count ceiling.

`budgets.v1.json` is the reviewed authority for canonical pixel tolerance. A
certified baseline must record that policy value. A candidate may request a
smaller value, but its requested, effective, and per-capture thresholds must be
consistent and may never exceed policy. The comparator recomputes pass/fail
from the recorded diff and the reviewed threshold; a manifest's stored
`passed` value is evidence consistency, not policy authority. The visual
package's 1% hard maximum remains an independent upper bound.

No browser baseline is implied by this policy file. Canonical comparison still
requires separately captured, source-bound browser and pixel evidence.
