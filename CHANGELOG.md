# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.2.0] - 2026-08-11

### Added

- Snapshot compression via [WithCompression] using stdlib `compress/gzip`.
  Compressed snapshots use the `.trace.gz` extension. Decompress with
  `gunzip` before analysis — `go tool trace` does not read gzip directly.
  A level of 0 (the default) disables compression.
  (`recorder.go`)
- Snapshot-to-directory capture: [WithSnapshotDir] configures a directory and
  [Recorder.SnapshotToDir] writes each snapshot to an auto-generated,
  timestamped filename (`<prefix><unix-nano>.trace`). Unlike SnapshotToFile,
  SnapshotToDir is not once-latched — every call produces a new file.
  (`recorder.go`)
- Snapshot retention: [WithMaxSnapshots] prunes the oldest snapshot files in
  the configured directory after each capture and once at Start. Cleanup
  failures are reported via the logger hook and never fail a snapshot.
  (`retention.go`)
- Configurable filename prefix via [WithSnapshotPrefix] (default `snapshot-`)
  for multi-instance coexistence in a shared directory. (`options.go`)
- Non-blocking capture: [Recorder.SnapshotIfAsync] evaluates a trigger and,
  if it fires, captures in a background goroutine. Stop and Close drain all
  in-flight captures before stopping, preventing a WriteTo/Stop data race.
  (`recorder.go`)
- Observability hooks: [WithMetrics] and [WithLogger] register dependency-free
  callbacks ([MetricsHook] and [LoggerHook]). The metrics hook receives a
  [SnapshotEvent] with duration, bytes, path, compression flag, source label,
  and the [TriggerContext.Kind]/[TriggerContext.Type] (for triggered/async
  captures) after every capture attempt. (`observe.go`)
- Nil-safe lifecycle methods: [Recorder.Enabled], [Recorder.Stop], and
  [Recorder.Close] are safe to call on a nil `*Recorder`, supporting the
  optional-recorder struct-field pattern. (`recorder.go`)
- `SnapshotToWriter`, a low-level escape hatch that writes the trace buffer
  directly to an arbitrary `io.Writer`, bypassing the configured sink and the
  once-latch (e.g., for a `/debug/trace` endpoint). (`recorder.go`)
- Comprehensive test coverage for all new features: compression, retention,
  directory snapshots, async capture + drain, metrics/logger hooks, nil-safe
  receivers, Kind/Type threading, concurrent stress, shutdown race, and an
  integration test. 64 tests pass with `-race`.
  (`recorder_test.go`)

### Changed

- Validation now rejects invalid compression levels and negative MaxSnapshots
  values with a [*ConfigError]. (`options.go`)
- Start prunes stale snapshot files from a previous process when retention is
  configured. (`recorder.go`)
- [SnapshotEvent] now carries [TriggerContext.Kind] and [TriggerContext.Type]
  fields so metrics hooks can label by operation. (`observe.go`)

### Fixed

- [Recorder.SnapshotIfAsync] returns `false` instead of `true` when the
  recorder is shutting down. Previously it returned `true` ("capture
  initiated") but silently dropped the capture — a lying return value in a
  debugging tool. (`recorder.go`)


## [0.1.1] - 2026-08-10

### Added

- Typed error system: `ConfigError`, `AlreadyEnabledError`, and
  `SnapshotError` types with `errors.As` support and `errors.AsType`
  helper for generic typed extraction (`errors.go`)
- `SECURITY.md` with private vulnerability reporting policy and
  response timeline (`SECURITY.md`)
- `TestRecorder_CloseIdempotentAfterSnapshot` regression test
  (`recorder_test.go`)

### Changed

- Updated package documentation with typed error examples (`doc.go`)
- Removed stale "zero dependencies" and Go Report Card references
  (`README.md`, `CONTRIBUTING.md`)

### Fixed

- `lazyFile.Close` now nils out the file handle after closing, making
  `Recorder.Close` truly idempotent — a second `Close` after a snapshot
  no longer returns "file already closed" (`options.go`)

## [0.1.0] - 2026-08-10

### Added

- Initial extraction from go-cqrs-lite as a standalone, zero-dependency library
- `Recorder` type wrapping `runtime/trace.FlightRecorder` with safe lifecycle
  management (`Start`, `Stop`, `Close`, `Enabled`) (`recorder.go`)
- Process-global singleton enforcement via `ErrAlreadyEnabled` (`recorder.go`)
- Snapshot capture with once-semantics to prevent concurrent snapshot races
  (`recorder.go`)
- `SnapshotToFile` for direct file-based capture (`recorder.go`)
- `Reset` to re-arm the snapshot latch for multiple captures (`recorder.go`)
- `SnapshotIf` for trigger-based conditional capture (`recorder.go`)
- Context cancellation support (pre-write check) for `Snapshot` and
  `SnapshotToFile` (`recorder.go`)
- Functional options: `WithMinAge`, `WithMaxBytes`, `WithWriter`, `WithFile`
  (`options.go`)
- `lazyFile` type for deferred file creation on first snapshot write
  (`options.go`)
- Config validation rejecting invalid `MinAge` and `MaxBytes` values
  (`options.go`)
- Composable trigger system: `OnLatency`, `OnError`, `OnErrorOrLatency`,
  `OnAlways`, `OnAny`, `OnAll` (`trigger.go`)
- `TriggerContext` data carrier with `Kind`, `Type`, `Duration`, `Err` fields
  (`trigger.go`)
- Full test suite: 27 test functions, all passing with `-race`
- Package documentation with quick-start examples (`doc.go`)
- `.golangci.yml` with ~90 linters (golangci-lint v2)
- GitHub Actions CI workflow (test + vet + lint) (`.github/workflows/ci.yml`)
- `CONTRIBUTING.md` with development setup and constraints
- `.editorconfig` and `.gitattributes` for cross-platform consistency
- `AGENTS.md`, `FEATURES.md`, `TODO_LIST.md`, `ROADMAP.md`

### Changed

- Inlined `openFile` into `captureToFile` — removed single-caller abstraction,
  `os.Create` is now called directly with consistent error wrapping
  (`recorder.go`)
- Updated `.gitignore` to include `reports/` and `coverage.out`

### Fixed

- All `r.Start()` calls in tests now check error return values (errcheck)
