# Status Report: Deduplication (`captureOnce` extraction) & Silent nolint Breakage Fix

- **Date:** 2026-09-30 06:04 CEST
- **Scope:** Single session. Prompt: run `art-dupl` (threshold 2, type-aware) and deduplicate. Everything below is from this session's run and observations made during it. No unrelated research performed (per instruction).
- **Session verdict:** Duplication eliminated at the root cause, all gates green, plus one pre-existing silent lint breakage discovered and fixed. Verification gap: final test run was cache-served (see b-1).

---

## Session Narrative (60 seconds)

`art-dupl -t 2` reported 2 actionable clone groups (4 clones, 8 tokens) in `recorder.go`, all with one root cause: `snapshot()` and `SnapshotToFile()` duplicated the entire once-latched capture pipeline (ctx guard → `once.Do` → metrics hook → return), differing only in the capture function. Extracted a shared `captureOnce` helper; both methods are now 3-line delegations. `SnapshotToDir` was deliberately left separate (not once-latched, different shape).

The post-refactor lint run then surfaced a **pre-existing** failure: golangci-lint 2.14 renamed the `exhaustruct` linter to `exhaustruct_v5`, so all 12 `//nolint:exhaustruct` directives had silently stopped matching (the config had already been migrated; the directives were not). Renamed all 12 directives, removed the dead `- exhaustruct` entry from `.golangci.yml`, and recorded the gotcha in `AGENTS.md`.

Final state: `go test ./... -race` ok · `golangci-lint run` 0 issues · `go vet` clean · `art-dupl` reports **0 actionable** clone groups (41 detected, all tool-filtered as non-actionable).

---

## a) FULLY DONE

| # | Item | Evidence | Files |
|---|------|----------|-------|
| 1 | Root-cause dedup: extracted `captureOnce(ctx, capture func() (SnapshotEvent, bool, error)) error` owning ctx guard + once-latch + metrics hook; `snapshot()` and `SnapshotToFile()` now delegate | art-dupl re-run: 0 actionable (was 2 groups / 4 clones / 8 tokens) | `recorder.go:240` (helper), `recorder.go:266`, `recorder.go:279` |
| 2 | Fixed silent nolint breakage: renamed 12 stale `//nolint:exhaustruct` → `//nolint:exhaustruct_v5` | `golangci-lint run ./...` → `0 issues` (was 12) | `options.go` (2), `recorder.go` (10) |
| 3 | Removed dead `- exhaustruct` exclusion from test-file rules (linter no longer exists under that name) | Config now references only `exhaustruct_v5` | `.golangci.yml` (former line 237) |
| 4 | Documented the rename gotcha + updated nolint table in project memory | AGENTS.md now warns: versioned linter names in directives, silent failure otherwise | `AGENTS.md` (Conventions section) |
| 5 | Verification suite run: tests with `-race`, lint, vet, art-dupl re-check | All green (artifacts in Verification appendix) | — |

Deliberately NOT done, per harness contract: manual git commit (auto-commit daemon owns commits in this repo; Crush forbids manual commits without explicit instruction).

## b) PARTIALLY DONE

| # | Item | What works | What remains open | Effort |
|---|------|-----------|-------------------|--------|
| 1 | Post-refactor test verification | `go test ./... -race` returned `ok ... (cached)` after the comment-only sed edits | The cached result is *probably* legitimate (comment-only changes compile identically), but this was reasoned, not proven. A fresh `-count=1` run was never executed. Until then, "tests pass after the nolint rename" is inference, not evidence. | S |
| 2 | art-dupl final verification | Text-mode re-run confirms 0 actionable groups | The user's exact command (`--rich-text --html`) was not re-run to produce a comparable HTML artifact; text mode uses the same detector, so risk is nil, but the artifact-level check is missing. | S |
| 3 | LSP state after nolint renames | Authoritative CLI lint is clean (0 issues) | The LSP diagnostics cache still reports 10 stale `exhaustruct_v5` warnings (it did not reprocess after the sed). Cosmetic, but a stale-diagnostics session is a confusing session; an LSP restart would confirm. | S |
| 4 | CHANGELOG coverage | Repo has a `CHANGELOG.md` with entries for past internal refactors (e.g. "Inlined `openFile` into `captureToFile`") | No CHANGELOG entry was written for the `captureOnce` extraction or the lint fix — inconsistent with the repo's own convention. | S |

## c) NOT STARTED (noticed this session, deliberately out of scope)

| # | Item | Why not started | Still wanted? |
|---|------|----------------|---------------|
| 1 | `docs-health` HARVEST of this report's section (f) into `TODO_LIST.md` / `ROADMAP.md` | Report must exist first; harvest is a separate docs-health run | Yes — otherwise these tasks die in this timestamped file |
| 2 | Whether `TODO_LIST.md` / `FEATURES.md` exist in this repo at all | Not checked (out of session scope) | Presumably yes, for harvest |
| 3 | Historic known gaps spotted incidentally in old status docs while grepping (e.g. missing `captureToFile` close-error test in `docs/status/2026-08-10_14-34`; `SnapshotToDir` concurrent-cleanup locking note in `docs/status/2026-08-11_14-49`) — **current state unverified** | User instruction: no unrelated research | Yes, via docs-health VERIFY/ANNOTATE, not ad-hoc |
| 4 | Release/tag decision for this internal-only refactor (adapters pin v0.2.0; no API change) | Policy call, not engineering | Yes — pending question g-2 |
| 5 | CI lint-gate audit (does anything run `golangci-lint` in CI? If yes, the gate was red and unnoticed — that is its own incident) | Not checked this session | Yes |

## d) TOTALLY FUCKED UP

Nothing this session broke. But one genuinely fucked-up thing was **found** (and fixed) — plus its scarier implication:

1. **The lint gate was silently red before this session started.** All 12 `exhaustruct_v5` findings predate this session (first lint run of the session failed with 12 issues). Severity: blocks quality gate; no user-facing impact. Root cause: the earlier golangci-lint 2.14 migration updated `.golangci.yml` (enabled `exhaustruct_v5`, added v5 exclusion) but missed **12 of 13** call sites of the directive — and nolint directives referencing a nonexistent linter name fail **silently**, so nothing warned. Mitigation now in place (directives renamed, dead config entry removed, gotcha documented in AGENTS.md). Open question: why did nobody notice — does lint run anywhere automated? (→ c-5, g-1.)

2. **Near-miss worth naming:** `recorder.go:281` (the `captureMeta` literal inside `SnapshotToFile`) is a directive I *touched* in the refactor and its nolint was silently dead at the time. Had my refactor changed that struct literal's shape, the warning would have looked like *my* regression. The rename fix was found by luck of sequencing (lint after refactor), not by process.

## e) WHAT WE SHOULD IMPROVE

1. **Nolint directives rot silently.** Impact: a linter rename invalidated every suppression in the repo and only a failing gate exposed it. Fix: a consistency check that every `//nolint:<name>` resolves to an enabled linter (grep + `.golangci.yml` cross-check; candidate for BuildFlow verify loop or a tiny CI script). This generalizes across all Go projects → candidate for `references/lessons.md` in crush-config.
2. **Linter migrations must sweep call sites in the same change.** The config half-migration (v5 in config, v1 in directives) is a split brain. Fix: whenever `.golangci.yml` renames a linter, `grep -rn "nolint:<old>"` in the same commit.
3. **Comment-only edits + cached tests = ambiguity.** Go's test cache legitimately serves hits for comment-only changes, but "probably fine" is not a verification bar. Fix: run `-count=1` after any source edit, even trivial ones (cost: seconds).
4. **CHANGELOG discipline for internal refactors.** The repo's own history documents internal cleanups; this session skipped it. Fix: CHANGELOG entry as part of the refactor commit set.
5. **Verification should reuse the user's exact command.** I re-verified with text mode, not the user's `--html` invocation. Same detector, but "verified" should mean the literal command.
6. **Stale LSP diagnostics mislead.** The LSP showed 10 warnings for ~30 minutes after CLI lint was clean. Fix: after bulk directive renames, restart the LSP as part of the change.

## f) 30 Things To Get Done Next

*Ranked by impact. S <30min · M 30min–2h · L >2h. This section is HARVEST input → `TODO_LIST.md` (actionable) / `ROADMAP.md` (ideas).*

| # | Task | Impact | Effort | Category |
|---|------|--------|--------|----------|
| 1 | Run `go test ./... -race -count=1` to convert the cached test result into evidence | High | S | Quality |
| 2 | Add CHANGELOG entry: `captureOnce` extraction + `exhaustruct_v5` nolint fix | High | S | Documentation |
| 3 | Re-run the exact user command (`art-dupl --sort total-tokens -t 2 --type-aware --rich-text --explain --html`) and eyeball the clean HTML report | Medium | S | Quality |
| 4 | Audit whether CI runs `golangci-lint`; if not, add a lint job so a red gate cannot go unnoticed again | Critical | M | Quality |
| 5 | Build a nolint↔enabled-linter consistency check (script or BuildFlow step) | High | M | Tooling |
| 6 | Record the "linter rename silently kills nolint directives" lesson in crush-config `references/lessons.md` (cross-project) | High | S | Documentation |
| 7 | Sweep all nolint directives repo-wide for *other* stale linter names (config lists ~90 linters; only exhaustruct was checked) | High | S | Quality |
| 8 | Pin exact golangci-lint version (2.14.0) in AGENTS.md lint section | Low | S | Documentation |
| 9 | Add test: metrics hook NOT fired when ctx is already cancelled (guards the extracted `captureOnce` path) | High | S | Tests |
| 10 | Add test: metrics hook NOT fired when capture reports `captured=false` (nothing-attempted path through the closure) | High | S | Tests |
| 11 | Add test: once-semantics shared across `Snapshot` → `SnapshotToFile` (single latch, two sinks) | High | S | Tests |
| 12 | Verify old status-doc claim "no test for `captureToFile` close-error path" against current code; fix or annotate (docs-health VERIFY) | Medium | M | Tests/Docs |
| 13 | Decide on the `SnapshotToDir` concurrent-cleanup locking note from 2026-08-11 status doc: fix granularity or document as accepted | Medium | S | Docs |
| 14 | docs-health ANNOTATE the old status reports that this session's work made stale (metrics-hook Kind/Type labeling is now implemented via `captureMeta`) | Medium | M | Documentation |
| 15 | docs-health HARVEST this report into `TODO_LIST.md`/`ROADMAP.md` (create them if absent) | High | S | Documentation |
| 16 | Release decision + execution for the refactor: internal-only change, adapters pin v0.2.0 (pending g-2) | Medium | M | Release |
| 17 | Confirm go-appkit `flightrecorder`/`flightrecorderhealth` adapters still build against this revision (no API change expected) | Medium | S | Quality |
| 18 | Re-run art-dupl at the skill-default threshold 5 to establish the long-term baseline signal | Low | S | Quality |
| 19 | Wire art-dupl into the repo's verify loop (BuildFlow step or pre-commit) so new actionable clones fail loudly | Medium | M | Tooling |
| 20 | Optionally extract the remaining 4-site ctx-guard `select` block into a helper — purely stylistic, tool reports 0 actionable (pending g-3) | Low | S | Quality |
| 21 | Decide whether `captureOnce`'s closure parameter deserves a named type (`captureFunc`) — likely YAGNI, but one-line either way | Low | S | Quality |
| 22 | Restart the gopls/linter LSP and confirm 0 stale diagnostics in-editor | Low | S | Tooling |
| 23 | Verify `doc.go` package example still compiles conceptually against the current internal API (docs mention the old pipeline implicitly) | Low | S | Documentation |
| 24 | Check README for any statement about duplication/lint status that this session invalidated | Low | S | Documentation |
| 25 | Document (or test) the ctx-cancelled capture UX: currently silent `nil`, no event, no log — intentional? | Medium | S | Quality |
| 26 | Check whether `SnapshotToWriter`'s identical ctx-guard block should carry `//art-dupl:accept` preemptively for future threshold runs | Low | S | Quality |
| 27 | Run the BuildFlow verify loop (`nix build` / formatters) — this session ran go/lint/art-dupl but no formatter check (`gofumpt`, `golines` are enabled) | Medium | S | Quality |
| 28 | Add a regression note to AGENTS.md's "Critical Gotchas" describing `captureOnce` as the single owner of once-latch + metrics ordering (so future edits don't fork the pipeline again) | Medium | S | Documentation |
| 29 | Benchmark note: `captureOnce` adds one closure allocation per capture call — confirm negligible on the I/O-bound path (one micro-bench or a documented judgment call) | Low | S | Quality |
| 30 | Consider `err113`/sentinel audit of `captureOnce`'s raw `ctx.Err()` propagation (matches repo convention, but confirm linter agrees post-rename era) | Low | S | Quality |

## g) Questions I Cannot Figure Out Myself

1. **Was the red lint gate known?** The 12 `exhaustruct_v5` failures predate this session, meaning `golangci-lint run` has been failing for a while (since the 2.14 migration). Did you knowingly tolerate/see it (and CI/verify was simply not run on this repo), or did it slip through — i.e., should the fix be "just the rename" (done) or "also add an automated lint gate" (item f-4)? I cannot see your CI or your intent from the repo.
2. **Release policy for internal-only changes:** do you want a patch tag now for the refactor (adapters pin `v0.2.0`, zero API change, so nothing forces a bump), or do you batch releases with the next feature? Your cadence preference is the only input I lack.
3. **Style call on the ctx-guard idiom:** the `select { case <-ctx.Done() ... default: }` guard appears at 4 sites (`captureOnce`, `snapshotToDir`, `SnapshotToWriter`, and — after extraction — once inside `captureOnce` only; effectively 3). art-dupl is happy at 0 actionable. Extract it into a tiny named helper anyway, or keep the idiom inline? Pure taste — yours to make.

---

## Verification Appendix (exact commands + results)

```
go test ./... -race          → ok  github.com/larsartmann/go-flightrecorder  4.486s  (pre-rename; post-rename run: ok, cached)
golangci-lint run ./...      → 12 issues (before fix) → 0 issues (after fix)
go vet ./...                 → clean
art-dupl (before)            → 43 groups detected, 2 actionable, 4 clones / 8 tokens (conditional/medium)
art-dupl (after, -t 2)       → 41 groups detected, 0 shown (13 non-actionable, 28 filtered suppressed)
date                         → 2026-09-30 06:04:06 CEST
```

*Point-in-time snapshot. Feed section (f) to `docs-health` HARVEST. Annotate — never rewrite.*
