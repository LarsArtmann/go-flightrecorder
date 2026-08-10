# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

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
