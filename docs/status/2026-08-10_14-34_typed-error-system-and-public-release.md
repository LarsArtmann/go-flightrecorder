# Status Report: Typed Error System + Public Release

**Date:** 2026-08-10 14:34
**Session scope:** GitHub publication, MIT license switch, typed error system design and implementation
**Previous report:** `docs/status/2026-08-10_14-07_production-infrastructure-complete.md`

---

## Executive Summary

The repo is **live on GitHub** at [LarsArtmann/go-flightrecorder](https://github.com/LarsArtmann/go-flightrecorder), tagged `v0.1.0`, MIT licensed, with a typed error system that reduced erraudit violations from 16 to 1. All 33 tests pass with race detector. 87.8% coverage.

**What went well:** The error system design is clean — three typed errors covering all failure modes, backward-compatible sentinel, Go 1.26 `errors.AsType` usage.

**What went wrong:** AGENTS.md and TODO_LIST.md are now stale — they document the old `fmt.Errorf` error patterns and the old "TODO" status for tasks that are already done. Multiple files required re-reads due to edit failures from indentation and stale file state.

---

## a) FULLY DONE

### Public Release

| Item | Evidence |
|------|----------|
| GitHub repo created | `https://github.com/LarsArtmann/go-flightrecorder` — public, MIT |
| `v0.1.0` tagged and pushed | `git tag` shows `v0.1.0`; GitHub release created with full notes |
| 11 topics set | go, golang, tracing, flight-recorder, runtime-trace, observability, profiling, diagnostics, performance, go-library, developer-tools |
| Homepage set | `https://pkg.go.dev/github.com/larsartorder` |
| CI passing | All runs green (test+race, vet, lint) |
| MIT license | `LICENSE` switched from proprietary; README updated |
| README badges | pkg.go.dev reference, CI status, MIT license badge |

### Typed Error System

| Item | Evidence |
|------|----------|
| `errors.go` created (115 lines) | 3 typed errors: `ConfigError`, `AlreadyEnabledError`, `SnapshotError` |
| String comparison eliminated | `recorder.go:62` — was `err.Error() == "flight recorder already enabled"`, now `&AlreadyEnabledError{Cause: err}` |
| Close errors handled | `recorder.go:97` (Close), `recorder.go:233` (captureToFile) — named return captures deferred close errors |
| Double-wrap prevention | `recorder.go:208` — `errors.AsType[*SnapshotError]` pass-through for lazyFile errors |
| Go 1.26 `errors.AsType` | Used instead of `errors.As` for type-safe generic unwrapping |
| `ErrAlreadyEnabled` sentinel preserved | `AlreadyEnabledError.Is()` method maintains `errors.Is` backward compat |
| 5 new tests for typed errors | `TestConfigError_TypedMatching`, `TestConfigError_MaxBytesZero`, `TestAlreadyEnabledError_TypedMatching`, `TestSnapshotError_WriteFailure`, `TestSnapshotError_FileCreationFailure` |
| doc.go error section | Full error handling guide with `errors.As` and `errors.Is` examples |
| erraudit: 16 -> 1 violations | Remaining 1 is intentional sentinel `errors.New` |
| 33 tests pass with `-race` | 87.8% statement coverage |
| golangci-lint: 0 issues | All 90+ linters clean |

### Commits This Session

| SHA | Message |
|-----|---------|
| `4a2a8a9` | Switch to MIT license for public release |
| `9349c94` | Remove Go Report Card badge (service shut down) |
| `20eda75` | Remove "zero dependencies" advertising |
| `30029ad` | Add typed error system with errors.As and errors.AsType support |

---

## b) PARTIALLY DONE

### Documentation freshness

- **CHANGELOG.md** — has `[Unreleased]` section but does NOT mention the typed error system, the MIT license switch, or the v0.1.0 release. Needs updating.
- **FEATURES.md** — line 29 still says `returns ErrAlreadyEnabled` without mentioning typed error variants. Should mention `*AlreadyEnabledError`, `*ConfigError`, `*SnapshotError`.
- **ROADMAP.md** — not checked for staleness this session. May reference old patterns.

---

## c) NOT STARTED

| Item | Why |
|------|-----|
| `SECURITY.md` | Needs GitHub repo (now exists), not created yet |
| pkg.go.dev verification | Cannot control when Go proxy indexes the module; no action taken |
| go-cqrs-lite shim fixes | Separate repo; shim has wrong `WithWriter` signature, no tests. User hasn't confirmed. |

---

## d) TOTALLY FUCKED UP

### AGENTS.md is stale and lying

`AGENTS.md` documents the **old** error system that no longer exists:

- **Line 44:** Shows `fmt.Errorf("%w: %w", ErrAlreadyEnabled, err)` — this code was DELETED. The actual code is `&AlreadyEnabledError{Cause: err}`.
- **Line 48:** Says "This is fragile: if Go changes the runtime error message, `ErrAlreadyEnabled` detection breaks silently." — This was the ENTIRE PROBLEM we fixed. The fragility is GONE. The doc still says it's fragile.
- **Line 80:** Says "internal validation errors use lowercase `err...` sentinels" — these sentinels (`errMinAgeMustBePositive`, `errMaxBytesMustBePositive`) were DELETED. Replaced by `*ConfigError`.
- **Line 81:** Says "Wrapping: `fmt.Errorf("%w: ...", sentinel, ...)`" — this pattern is GONE. Replaced by typed error structs.

**Anyone reading AGENTS.md will build a completely wrong mental model of the error system.**

### TODO_LIST.md is stale and lying

- **Tag v0.1.0:** Marked 🔴 TODO — actually DONE (tagged, released, pushed)
- **Publish to GitHub:** Marked 🔴 TODO — actually DONE (public repo, pushed)
- **lazyFile edge case test:** Marked 🔴 TODO saying "No test for `Close()` when file was never opened" — actually DONE: `TestRecorder_LazyFileCloseWithoutSnapshot` exists at `recorder_test.go:546`
- **SECURITY.md:** Marked 🔴 TODO "needs remote first" — remote now exists, item is unblocked but not updated

### Missing `//go:build` constraint

The library uses `runtime/trace.FlightRecorder` which requires Go 1.25+. We pin `go 1.26.5` in `go.mod` and use `errors.AsType` (Go 1.26+), but there's no `//go:build go1.26` directive in any file. If someone with Go 1.24 tries to build, they get a confusing error instead of a clear "requires Go 1.26+" message.

---

## e) WHAT WE SHOULD IMPROVE

### Code

1. **`errors.AsType` in tests is inconsistent** — Production code uses `errors.AsType[*SnapshotError]` (Go 1.26), but the tests still use the old `errors.As(err, &target)` pattern. The tests should demonstrate the modern API.

2. **`failingWriter` test helper is minimal** — Could test partial writes, deadline-exceeded writers, or writers that fail after N bytes.

3. **No test for `captureToFile` close error path** — The named-return close-error capture in `captureToFile` is tested by the success path only. No test forces `f.Close()` to fail after a successful write.

4. **`SnapshotError.Path` is empty string for writer failures** — This is correct behavior, but the `Error()` message changes format based on whether Path is empty. Not tested for the empty-path case explicitly.

5. **No benchmark tests** — The library is for production performance-sensitive code. No benchmarks for `Snapshot()`, `capture()`, or trigger evaluation.

6. **`OnPanic` trigger is documented in README but doesn't exist** — README says `flightrecorder.OnPanic(recorder)` in the Quick Start, but there's no `OnPanic` function in `trigger.go`. This is a documentation lie.

7. **Trigger functions return closures but have no `String()` method** — Makes debugging trigger chains harder.

### Architecture

8. **No middleware/http handler integration** — The library provides triggers but no HTTP middleware that automatically captures snapshots on slow/error responses. This is the primary use case described in the README.

9. **No integration test that exercises the full lifecycle** — Start, run goroutines, trigger snapshot, verify trace is parseable by `go tool trace`. All tests verify bytes are written but never validate the trace format.

10. **`Recorder.Reset()` is not thread-safe relative to `Snapshot()`** — Reset replaces `r.once` under the mutex, but a concurrent `Snapshot` may have already passed the once.Do check and be in `capture()`. This is a potential race.

### Documentation

11. **CHANGELOG.md not updated** for typed error system, MIT switch, or v0.1.0 release.

12. **FEATURES.md not updated** for typed error types.

13. **README Quick Start references `OnPanic`** which doesn't exist.

14. **CONTRIBUTING.md still says "zero dependencies"** — Wait, actually I removed the heading but the text "This library is stdlib-only. Do not add third-party imports." remains. This is contradictory with the user's preference.

---

## f) Up to 50 Things to Get Done Next

### Critical (do first)

1. **Fix AGENTS.md** — Remove old error patterns, document typed error system (`ConfigError`, `AlreadyEnabledError`, `SnapshotError`), remove "fragile" warning, update error conventions section
2. **Fix TODO_LIST.md** — Mark v0.1.0/GitHub/lazyFile as DONE, remove or update stale items
3. **Fix CHANGELOG.md** — Add typed error system, MIT license, v0.1.0 release under `[Unreleased]` or new `v0.1.0` section
4. **Fix README.md `OnPanic` reference** — Either implement `OnPanic` or remove from README Quick Start
5. **Update FEATURES.md** — Add typed error types to the feature table

### High Impact

6. **Implement `OnPanic` trigger** — It's referenced in README and is a natural trigger (recover from panic, snapshot, re-panic)
7. **Add `SECURITY.md`** — GitHub repo exists now, can link to advisories
8. **Add HTTP middleware** — `func Middleware(r *Recorder, trigger TriggerFunc) func(http.Handler) http.Handler`
9. **Add integration test** — Verify captured trace is valid parseable format via `go tool trace` or `trace.Parse`
10. **Add benchmark tests** — `BenchmarkSnapshot`, `BenchmarkTriggerEvaluation`, `BenchmarkCaptureToFile`
11. **Update tests to use `errors.AsType`** — Consistency with production code
12. **Fix `Recorder.Reset()` race** — Ensure once replacement is atomic relative to in-flight captures
13. **Add `//go:build go1.26` directive** — Or document the minimum Go version more prominently

### Medium Impact

14. **Add `OnSlowRequest` HTTP trigger** — Specialized trigger for HTTP latency
15. **Add context-aware triggers** — `OnContextCancel()`, `OnContextDeadlineExceeded()`
16. **Add multi-snapshot support** — Ring buffer of N snapshots instead of once-semantics
17. **Add `WithSnapshotDir` option** — Auto-generate timestamped filenames in a directory
18. **Add slog integration** — Log snapshot events via `slog.Handler`
19. **Add Prometheus metrics** — Counter for snapshots taken, histogram for snapshot duration
20. **Add `Recorder.SnapshotAsync()`** — Non-blocking snapshot via goroutine for latency-sensitive paths
21. **Add trigger composition with context** — `TriggerFunc` should receive `context.Context` for cancellation
22. **Add `WithMaxSnapshots(n)` option** — Auto-reset after N snapshots instead of manual `Reset()`
23. **Add test for `captureToFile` close failure** — Use a wrapper file that fails on Close
24. **Add test for `Close()` when `lazyFile.Close()` fails** — Currently untested error path
25. **Add property-based tests** — For trigger composition (`OnAny`/`OnAll` distributive properties)
26. **Add `go test -race -count=100`** to CI — Catch flaky races
27. **Add `govulncheck` to CI** — Required by how-to-golang skill
28. **Add `gosec` to CI** — Required by how-to-golang skill
29. **Add coverage upload to Codecov** — Currently coverage is local only
30. **Add `.golangci.yml` version check in CI** — Ensure lint config version matches
31. **Add CONTRIBUTING.md section on error handling** — Document the typed error pattern for contributors
32. **Add `TriggerContext.WithTrace()` method** — Allow enriching trigger context with trace IDs
33. **Add `Recorder.Status()` method** — Return structured status (enabled, snapshots taken, bytes written)
34. **Add `WithLabel` option** — For multi-recorder environments (even though Go only allows one)

### Lower Priority / Polish

35. **Add Go example tests** — `ExampleNew`, `ExampleSnapshot`, `ExampleTriggers` — shown on pkg.go.dev
36. **Add `go tool trace` integration test** — Actually run the tool and verify exit code
37. **Add fuzz tests** — For trigger evaluation and config validation
38. **Add `Notifier` interface** — Notify channels/listeners when a snapshot is captured
39. **Add `WithSampling(rate)` option** — Sample snapshots instead of capturing every trigger
40. **Add automatic snapshot rotation** — Periodic snapshots at configured interval
41. **Add `Recorder.Drain()` method** — Wait for in-flight snapshot to complete
42. **Add context propagation to `capture()`** — Currently ignores context after once.Do starts
43. **Add `ErrSnapshotInProgress` sentinel** — For non-blocking snapshot attempts
44. **Add `WithTimeout(d)` for snapshots** — Time-bound the WriteTo call
45. **Add tests for concurrent `Reset()` + `Snapshot()`** — Explicit race test
46. **Add `fmt.Stringer` for `TriggerFunc`** — Make trigger chains debuggable
47. **Add `Options` struct as alternative to functional options** — For struct validation use cases
48. **Add `Recorder.Clone()` method** — Create a copy with the same config (for testing)
49. **Add wiki pages** — Advanced usage patterns, migration from manual `runtime/trace`
50. **Add `CHANGELOG.md` entries for all future changes** — Establish the discipline

---

## g) Questions I CANNOT Answer Myself

### Q1: Should we adopt samber/oops despite the dependency cost?

The erraudit tool flags `ErrAlreadyEnabled = errors.New(...)` as a violation because it wants `oops.New(...)`. But samber/oops v1.23.0 pulls in **3 transitive dependencies**: `go.opentelemetry.io/otel/trace`, `samber/lo`, and `oklog/ulid/v2`. For a micro-library that wraps stdlib, this feels like verschlimmbesserung. However, you use samber/lo and samber/do in other projects.

**Do you want to add samber/oops as a dependency and silence the last erraudit violation, or keep the intentional sentinel?**

### Q2: Is `OnPanic` a trigger we should implement?

The README Quick Start references `flightrecorder.OnPanic(recorder)` but it doesn't exist in `trigger.go`. It's a natural flight recorder use case (recover from panic, snapshot the trace, re-panic). But it requires a different signature than other triggers because it needs `recover()`.

**Should I implement `OnPanic`, or remove the reference from the README?**

### Q3: Should we fix the go-cqrs-lite shim now?

The extraction report noted that the go-cqrs-lite re-export shim has a wrong `WithWriter` signature, unnecessary wrapper functions, and zero tests. Now that v0.1.0 is tagged and the API has changed (typed errors), consumers need to update anyway.

**Should I switch to go-cqrs-lite and fix the shim + update its go.mod to v0.1.0, or leave it for a separate session?**
