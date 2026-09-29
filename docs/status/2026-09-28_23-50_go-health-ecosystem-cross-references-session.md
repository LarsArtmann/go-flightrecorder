# Status Report — go-health Ecosystem Cross-References Session

- **Date:** 2026-09-28 23:50 CEST
- **Scope:** This session only — the "do we integrate with go-health?" investigation, the
  own-repo-vs-submodule advisory, and the documentation cross-reference work it produced
  across `go-flightrecorder`, `go-health` (and analysis of `go-appkit/flightrecorderhealth`).
- **Format note:** Status-report skill defaults to a styled HTML dashboard; the explicit
  user instruction (`docs/status/*.md`) wins, matching this repo's existing report series.

## Session narrative (what happened)

1. User asked whether `go-flightrecorder` integrates with `go-health`.
2. I searched both repos, found zero references, and answered **"no integration, and here
   is why not"** — with confident architectural reasoning. **That answer was wrong**: the
   adapter exists at `go-appkit/flightrecorderhealth` (v0.1.3, released, documented). The
   user had to hand me the path.
3. User asked what `go-appkit/flightrecorderhealth` is → I documented its two integration
   points (`Checkable`/`Register`, `Trigger`/`NewTrigger`) and corrected my earlier claim.
4. User asked whether the adapter should become its own repo → I recommended **no**
   (zero coupling to appkit core, independent tagging already works, family-release
   coordination, 302-line scope), based on verified go.mod/doc.go reads.
5. User asked for doc cross-references → added to `go-flightrecorder` (README, ROADMAP,
   AGENTS.md) and mirrored into `go-health` (README Audit Integration, AGENTS.md, FEATURES.md),
   including correcting go-health's **false "only known consumer" claim** with a verified
   10-consumer inventory.

---

## a) FULLY DONE

| # | Work                                                                                                                                                                                                                                                                                                                                                                                     | Evidence                                                                                                                                 | Files                                 |
| - | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------- |
| 1 | go-flightrecorder README: new "Ecosystem" section listing both official adapters with pkg.go.dev links (bullets, matching house style)                                                                                                                                                                                                                                                   | Content verified in `git diff`; auto-commit daemon captured it (`M README.md` state observed, daemon pattern confirmed on sibling files) | `README.md:190`                       |
| 2 | go-flightrecorder ROADMAP: "Ecosystem Integration" theme gained a shipped-status `**Status:**` note (matching theme 3's existing pattern); already-delivered HTTP-middleware idea removed from raw ideas                                                                                                                                                                                 | Committed by daemon (`e383ce0`); `rg "Status.*Two official adapters" ROADMAP.md` → line 12                                               | `ROADMAP.md:8-24`                     |
| 3 | go-flightrecorder AGENTS.md: new "Official Adapters" section (adapter inventory + "must never import them" constraint + pointer rule for future sessions)                                                                                                                                                                                                                                | Committed by daemon (`e383ce0`); verified line 36                                                                                        | `AGENTS.md:36-41`                     |
| 4 | go-health README: Audit Integration section now shows `flightrecorderhealth.Trigger` as second implicit `HealthRecorder` implementor with minimal `NewTrigger(rec)` example                                                                                                                                                                                                              | Committed by daemon (`be241cb`/`4c88845`); `git show HEAD:README.md` spot-check verified the section verbatim                            | `README.md` (§ Audit Integration)     |
| 5 | go-health AGENTS.md: HealthRecorder bullet names both implementors; false "**only** known consumer is go-health-dashboard" corrected to "most closely verified consumer"; new dated consumer-inventory paragraph (10 consumers, verified via go.mod requires 2026-09-28, with honest "requires only, suites not re-run" caveat); API-coordination duty extended to appkit bridge modules | Committed by daemon; `git show HEAD:AGENTS.md` verified                                                                                  | `AGENTS.md` (§ consumer verification) |
| 6 | go-health FEATURES.md: HealthRecorder row lists both implementors; table pipe alignment preserved exactly                                                                                                                                                                                                                                                                                | `git diff` reviewed; all table rows verified at exactly 390 chars via python check                                                       | `FEATURES.md:82`                      |
| 7 | Advisory: "should flightrecorderhealth be its own repo?" answered with evidence-backed **no** (zero appkit-core coupling verified via `rg "go-appkit\""` across module; independent tagging v0.1.3; health-family train 2026-09-20; extraction = breaking import-path change)                                                                                                            | Analysis delivered in-session; grounded in go.mod/doc.go/go-appkit-AGENTS.md reads                                                       | —                                     |
| 8 | Claim hygiene: every encoded claim traced to a local source read this session (go.mod requires of 10 consumers; adapter doc.go/adapter.go symbols; appkit AGENTS.md family-train dates)                                                                                                                                                                                                  | Verification commands in session log                                                                                                     | —                                     |

## b) PARTIALLY DONE

| # | Work                                   | What works                                                                          | What remains                                                                                                                                                                                                                                                         | Effort |
| - | -------------------------------------- | ----------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------ |
| 1 | go-health consumer-record accuracy     | The false "only known consumer" sentence is fixed and the inventory paragraph added | Stale remnants survive: AGENTS.md history sentence still says dashboard "Bumped to released v0.4.0" (its go.mod now pins v0.4.1 — my sentence notes this but doesn't repair the history), and FEATURES.md "Consumer verification" row still claims v0.4.0 as current | S      |
| 2 | Consumer verification depth            | Requires verified for all 10 consumers (direct file reads)                          | No consumer suite was re-run — the caveat is written into AGENTS.md, but the verification itself is open work                                                                                                                                                        | M      |
| 3 | README example correctness (go-health) | `NewTrigger(rec)` example matches adapter's verified API surface                    | Example is hand-written and never compiled anywhere in go-health (no example_test.go wiring) — it can drift silently                                                                                                                                                 | S      |

## c) NOT STARTED

Deliberately out of scope this session ("just add references"); all noticed, none acted on:

| # | Item                                                                                                                                                                                                                | Why not started                                                                                                                                        |
| - | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1 | Re-run go-health-dashboard full suite against v0.4.1                                                                                                                                                                | Release-gate work, needs owner policy on scope                                                                                                         |
| 2 | Verify go-appkit `health` + `flightrecorderhealth` suites green against go-health v0.2.0                                                                                                                            | Same — consumer-suite verification was explicitly out of scope                                                                                         |
| 3 | Back-link from go-appkit README/adapter README to go-flightrecorder's new Ecosystem section                                                                                                                         | go-appkit was never asked to be modified; bidirectional linking incomplete                                                                             |
| 4 | go-flightrecorder TODO_LIST.md stale claim "v0.1.1 tagged and pushed; no high-impact tasks open" while v0.2.0 shipped 2026-08-11                                                                                    | Noticed during session; pre-existing staleness, not this session's task                                                                                |
| 5 | go-flightrecorder FEATURES.md stale line-number references (`recorder.go:45` etc.) and `SnapshotIf` vs README's `SnapshotIfAsync` naming inconsistency                                                              | Noticed; pre-existing, out of scope                                                                                                                    |
| 6 | go-flightrecorder README tagline "Go 1.25's runtime/trace.FlightRecorder" vs "Requires Go 1.26+" tension; README says go.mod "pins 1.26.5" while the directive reads `go 1.26`                                      | Noticed; cosmetic, out of scope                                                                                                                        |
| 7 | CHANGELOG entries for the doc-only changes in both repos                                                                                                                                                            | Judgment call: no `[Unreleased]` section existed in go-flightrecorder; doc cross-references arguably not changelog-worthy — never confirmed with owner |
| 8 | Click-verification that the three new pkg.go.dev links render (they are deterministic module paths; appkit AGENTS.md says submodule pages render, but I did not click)                                              | Low value vs cost                                                                                                                                      |
| 9 | `go-appkit/flightrecorderhealth` README "Build note: requires GOEXPERIMENT=jsonv2" looks stale now that jsonv2 is default-on (appkit AGENTS.md verified this 2026-09-04; go-health's Go 1.27 floor makes it stable) | Noticed while reading adapter README; go-appkit out of scope                                                                                           |

## d) TOTALLY FUCKED UP

Radical honesty section — these are the session's real failures:

| # | Failure                                                                                                                                                                                                                                                                                                                                    | Severity                                                                                                                              | Root cause                                                                                                                                                                                                                                                                                                                                              | Mitigation                                                                |
| - | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| 1 | **My first answer was factually wrong.** "No integration exists — zero references in either direction, and here is why not": while a released, documented adapter (`go-appkit/flightrecorderhealth` v0.1.3) sat one directory away. I then built a confident causal story ("deliberate zero-dep constraints") on top of the wrong premise. | High — misinformation in an advisory answer; if the owner had said "build that adapter," I would have duplicated shipped, tagged work | Searched only the two repos the user named (`grep "go-health"` in go-flightrecorder, `grep "flightrecorder"` in go-health). Never swept `~/projects/*/go.mod` for the module path — a 2-command check. Also failed to recall that go-appkit's AGENTS.md (a file I read later in the same session) documents the flightrecorderhealth module explicitly. | None applied yet — see improvement (e)(1) and task (f) "fleet-sweep rule" |
| 2 | README table churn: wrote a 4-row pipe table with misaligned separator/padding, then replaced it with bullets.                                                                                                                                                                                                                             | Low — wasted a cycle; dprint isn't installed here so the misalignment would have shipped                                              | Didn't check the repo's existing table style or formatter availability before writing                                                                                                                                                                                                                                                                   | Checked after; bullets match house style                                  |
| 3 | Edit-tool rejection on ROADMAP.md ("you must read the file before editing") — violated my own read-before-edit rule.                                                                                                                                                                                                                       | Low — one extra round trip                                                                                                            | Assumed content from an earlier `cat` in a different tool call                                                                                                                                                                                                                                                                                          | Re-read, then edited cleanly                                              |
| 4 | (Recurring, fleet-level) Consumer-verification records in go-health were allowed to go stale and false ("only known consumer") for at least 5 days and ~9 new consumers — discovered only because this session asked about integrations.                                                                                                   | Medium — future release decisions would rest on a false consumer map                                                                  | No periodic docs-health VERIFY pass on AGENTS.md consumer claims                                                                                                                                                                                                                                                                                        | Task (f): add consumer-inventory verify to release checklist              |

## e) WHAT WE SHOULD IMPROVE

1. **Fleet-sweep before negative claims (highest value).** "Do we integrate with X?" must trigger `rg "module-path" ~/projects/*/go.mod ~/projects/*/*/go.mod` before any "no" answer. Two commands would have caught the adapter instantly. Candidate for a hard rule in global AGENTS.md and/or a crush-config `references/lessons.md` entry (cross-project lesson → commit in crush-config repo, per memory rules).
2. **Asymmetry of confidence.** Distinguish "verified absent" from "not found in the places I looked," and never narrate confident _why-not_ causality on a negative existence claim. The wrong story was more damaging than the wrong fact — it sounded authoritative.
3. **Read-before-edit and house-style-before-write** — both lapsed once each this session (ROADMAP edit rejection; README table churn). Both are already rules; the gap is application, not knowledge.
4. **Doc-example rot.** README code examples in go-health (including my new one) are never compiled. A `example_test.go` per README snippet (or doc-snippet lint) would catch API drift.
5. **Split-brain I just helped create:** implementor/consumer lists now live in up to 3 files per repo (README, AGENTS.md, FEATURES.md). Next adapter = 6+ coordinated edits across 2 repos. Consider one canonical "Consumers & Implementors" list per repo with one-line pointers elsewhere.
6. **CHANGELOG policy ambiguity** for doc-only changes — decide once, stop re-deciding per change.
7. **Daemon trust with verification** — I relied on the auto-commit daemon twice and verified via `git status`/`git show HEAD:` both times (correct behavior; keep it — buildflow skill explicitly warns the daemon has blind spots).

## f) Up to 50 things we should get done next

Ranked by impact within category blocks; each item is harvestable into TODO_LIST.md
(specific + bounded) or ROADMAP.md (larger/idea-shaped). **This section is the primary
input for docs-health HARVEST — do not let it die in this timestamped file.**

### Cross-repo / fleet (highest leverage)

| #  | Task                                                                                                                                                                                                                   | Impact | Effort | Category      |
| -- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------ | ------ | ------------- |
| 1  | Make "fleet-sweep go.mod requires before answering any integration/absence question" a hard rule: add to global crush-config AGENTS.md + record in `references/lessons.md` (committed in crush-config, not in-session) | High   | S      | Process       |
| 2  | Re-run go-health-dashboard full suite (incl. browser screenshot tests) against pinned v0.4.1 so "most closely verified consumer" stays true                                                                            | High   | M      | Quality       |
| 3  | Verify go-appkit `health` + `flightrecorderhealth` full suites green against go-health v0.2.0 (clears the "requires only" caveat now in go-health AGENTS.md)                                                           | High   | M      | Quality       |
| 4  | Define release-gate policy: which of the 10 known go-health consumers must be green before tagging (see question g/Q1)                                                                                                 | High   | S      | Process       |
| 5  | Add a "consumers to bump" checklist step when tagging go-flightrecorder: appkit `flightrecorder` + `flightrecorderhealth` modules pin released tags                                                                    | High   | S      | Process       |
| 6  | Canonicalize consumer/implementor inventories: one owner file per repo (go-health: consumer list; go-flightrecorder: adapter list), other docs point at it — kills the 3-place split brain                             | Medium | M      | Quality       |
| 7  | Build each of the 8 app consumers once against their pinned go-health versions, or trim the AGENTS.md inventory to build-verified consumers                                                                            | Medium | L      | Quality       |
| 8  | Fleet convention: per-repo "ECOSYSTEM/Consumers" doc section standard so integration discoverability stops depending on grep luck                                                                                      | Medium | M      | Process       |
| 9  | Add markdown link-check (e.g. lychee) as a BuildFlow extension, not per-repo scripts (fleet-wide value per BuildFlow skill)                                                                                            | Medium | S      | Quality       |
| 10 | Back-link from go-appkit (README or adapter READMEs) to go-flightrecorder's Ecosystem section for bidirectional discoverability                                                                                        | Low    | S      | Documentation |

### go-health

| #  | Task                                                                                                                                               | Impact | Effort | Category      |
| -- | -------------------------------------------------------------------------------------------------------------------------------------------------- | ------ | ------ | ------------- |
| 11 | Fix FEATURES.md "Consumer verification" row: v0.4.0 claim → reflect go.mod v0.4.1 pin                                                              | Medium | S      | Documentation |
| 12 | Repair AGENTS.md history sentence "Bumped to released v0.4.0 on 2026-09-22" with a v0.4.1 verification addendum                                    | Medium | S      | Documentation |
| 13 | Compile-check the README Trigger example: add `example_test.go` wiring (or doc-snippet test) for `NewTrigger` usage                                | Medium | S      | Quality       |
| 14 | Sweep remaining README examples for compile-rot (auditlog example included) and wire them into tests                                               | Medium | M      | Quality       |
| 15 | Decide dprint.json's fate: treefmt runs gofumpt/gofmt/golines/nixfmt only — the dprint config appears vestigial                                    | Low    | S      | Cleanup       |
| 16 | Check whether any docs claim "dashboard is the only consumer" elsewhere (docs/, aggregate, federation docs) and align with the corrected inventory | Medium | S      | Documentation |

### go-flightrecorder

| #  | Task                                                                                                                                                                                                  | Impact | Effort | Category      |
| -- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------ | ------ | ------------- |
| 17 | Fix TODO_LIST.md stale status: "v0.1.1 tagged and pushed" → v0.2.0 shipped 2026-08-11                                                                                                                 | High   | S      | Documentation |
| 18 | Add adapter pointers to `doc.go` package docs (godoc is the first place integrators look)                                                                                                             | Medium | S      | Documentation |
| 19 | Reconcile FEATURES.md method naming: data-flow text says `SnapshotIf`, README documents `SnapshotIfAsync` (and AGENTS.md table lists both)                                                            | Medium | S      | Documentation |
| 20 | Refresh FEATURES.md line-number references against current recorder.go/options.go                                                                                                                     | Low    | S      | Quality       |
| 21 | Reconcile README install note "go.mod pins 1.26.5" vs actual `go 1.26` directive; also the "Go 1.25's" tagline vs "Requires Go 1.26+" tension                                                         | Low    | S      | Documentation |
| 22 | Add a compiled integration-flavored example (recorder + trigger evaluation loop) to `example_test.go` mirroring what the appkit adapters do                                                           | Medium | M      | Documentation |
| 23 | Click-verify the three new pkg.go.dev links render                                                                                                                                                    | Low    | S      | Documentation |
| 24 | Decide + document CHANGELOG policy for doc-only changes; if entries wanted, add retroactive `[Unreleased]` entry for the ecosystem docs                                                               | Low    | S      | Documentation |
| 25 | CONTRIBUTING.md: mention the ecosystem adapters and the "integration wiring lives in appkit-family modules" rule                                                                                      | Low    | S      | Documentation |
| 26 | Audit `docs/` and `reports/` for pre-adapter integration claims that are now wrong (same class of staleness as the ROADMAP gap)                                                                       | Medium | S      | Documentation |
| 27 | ROADMAP: note go-appkit modules as the canonical home for new HTTP/framework adapters; keep chi/echo/gin/gRPC ideas routed there                                                                      | Low    | S      | Documentation |
| 28 | Consider `doc.go` or README note on the `SnapshotToWriter`/once-semantics split (FEATURES lists it; README's two-methods-two-intents section could name it) — noticed while cross-reading, unverified | Low    | S      | Documentation |

### go-appkit (adjacent, noticed — was not modified this session)

| #  | Task                                                                                                                                                             | Impact | Effort | Category      |
| -- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------ | ------ | ------------- |
| 29 | Fix stale build note in `flightrecorderhealth/README.md` ("requires GOEXPERIMENT=jsonv2") — jsonv2 is default-on and stable at go-health's Go 1.27 floor         | Medium | S      | Documentation |
| 30 | Same check for `flightrecorder/README.md` and appkit root AGENTS.md GOEXPERIMENT mentions (AGENTS.md already says "drop when toolchain floor moves past 1.26.7") | Low    | S      | Documentation |
| 31 | Extend appkit's `documentedPins`/pin-drift guard idea cross-repo: alert when a bridge module pins a go-health/go-flightrecorder tag older than latest released   | Medium | M      | Quality       |
| 32 | go-appkit AGENTS.md modules list: consider one-line pointer "adapters of go-flightrecorder ↔ go-health — see those repos' Ecosystem sections"                    | Low    | S      | Documentation |

### Meta / tooling

| #  | Task                                                                                                                                                                             | Impact | Effort | Category |
| -- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------ | ------ | -------- |
| 33 | Capture this session's lesson as a cross-project entry in crush-config `references/lessons.md` (verify-before-claiming-absence class)                                            | High   | S      | Process  |
| 34 | Verify go-health FEATURES.md working-tree edit got committed by the daemon at next session start (pending at write time)                                                         | Medium | S      | Process  |
| 35 | Decide commit policy for doc-only changes: daemon-only (current) vs explicit commits with why-messages (see question g/Q3)                                                       | Medium | S      | Process  |
| 36 | Add "consumer inventory refresh" to go-health release checklist (AGENTS.md inventory re-verified each tag)                                                                       | Medium | S      | Process  |
| 37 | Mirror the same for go-flightrecorder: adapter-pin refresh check each tag                                                                                                        | Medium | S      | Process  |
| 38 | docs-health HARVEST of this report's (f) section into TODO_LIST.md/ROADMAP.md — scheduled next session start per status-report skill                                             | High   | S      | Process  |
| 39 | Sweep other fleet repos for the same "only known consumer"-style absolute claims and soften/verify them                                                                          | Medium | M      | Quality  |
| 40 | Consider a tiny `scripts/fleet-consumers.sh` (rg over ~/projects go.mods) as a personal tool — but prefer the skill/rule route (item 1) over scripts per BuildFlow anti-patterns | Low    | S      | Process  |

_(Items 41-50 intentionally left unstuffed: the remaining ideas — e.g. new adapter features, trigger enhancements — belong to go-flightrecorder's ROADMAP themes, not this session's harvest; inventing them here would be scope creep without fresh code-level verification.)_

## g) Questions I cannot figure out myself

**Q1 — Release-verification policy (unblocks the biggest open caveat).**
I built a 10-consumer inventory for go-health, but I cannot decide the risk policy:
when go-health (or go-flightrecorder) tags a release, which consumers MUST be re-verified
green before the tag ships — all 10, or a canonical subset (dashboard + the two appkit
bridge modules) with the rest verified per-go.mod only? Both AGENTS.md files historically
pin only the dashboard as the verified consumer; the app repos appeared this session and
nobody has defined their gate status.

**Q2 — Was my initial wrong answer a process failure you want hard-rules against?**
Should negative integration claims ("we don't integrate with X") be **banned** unless a
`~/projects/*/go.mod` fleet sweep was run (new hard rule in global AGENTS.md), or do you
consider naming the relevant repos enough scope that my answer was acceptable? I lean
"hard rule" (it costs two commands and would have prevented the session's only factual
failure), but rule adoption in your global config is your call — and if yes, whether it
belongs in global AGENTS.md or as a lesson entry in crush-config.

**Q3 — Commit policy for documentation changes.**
This session's doc work was captured by the auto-commit daemon under messages like
"chore: auto-commit N changed file(s) (heuristic)" — the _why_ (cross-repo ecosystem
cross-references, corrected false consumer claim) lives only in this report and the
diffs. Should doc-only session work be explicitly committed with real messages (daemon
as fallback), or is the daemon's heuristic trail acceptable for docs and explicit commits
reserved for code? This affects every future docs session and I can't infer your
preference from repo history (both patterns occur).

---

## Verification appendix (how claims above were checked)

- Consumer versions: direct reads of `go.mod` in go-appkit (`health`, `flightrecorderhealth`),
  go-health-dashboard, and 8 app repos — 2026-09-28.
- Adapter API surface: `go-appkit/flightrecorderhealth/adapter.go` exported-symbol list,
  `doc.go`, `README.md`.
- Family-train dates: `go-appkit/AGENTS.md` release-state section.
- Table alignment after FEATURES.md edit: python width check, all rows 390 chars.
- Commits: `git log` + `git show HEAD:` content spot-checks in both repos; go-health
  FEATURES.md was still `M` in the working tree at report time (daemon pending).
