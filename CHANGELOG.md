# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Initial extraction from go-cqrs-lite as a standalone, zero-dependency library
- `Recorder` type wrapping `runtime/trace.FlightRecorder` with safe lifecycle
  management (`Start`, `Stop`, `Close`, `Enabled`) (`recorder.go`)
- Process-global singleton enforcement via `ErrAlreadyEnabled` (`recorder.go:21`)
- Snapshot capture with once-semantics to prevent concurrent snapshot races
  (`recorder.go:132`)
- `SnapshotToFile` for direct file-based capture (`recorder.go:153`)
- `Reset` to re-arm the snapshot latch for multiple captures (`recorder.go:194`)
- `SnapshotIf` for trigger-based conditional capture (`recorder.go:176`)
- Context cancellation support (pre-write check) for `Snapshot` and
  `SnapshotToFile` (`recorder.go:133`)
- Functional options: `WithMinAge`, `WithMaxBytes`, `WithWriter`, `WithFile`
  (`options.go`)
- `lazyFile` type for deferred file creation on first snapshot write
  (`options.go:85`)
- Config validation rejecting invalid `MinAge` and `MaxBytes` values
  (`options.go:31`)
- Composable trigger system: `OnLatency`, `OnError`, `OnErrorOrLatency`,
  `OnAlways`, `OnAny`, `OnAll` (`trigger.go`)
- `TriggerContext` data carrier with `Kind`, `Type`, `Duration`, `Err` fields
  (`trigger.go:8`)
- Full test suite: 27 test functions (38 including subtests), all passing
- Package documentation with quick-start examples (`doc.go`)
- `AGENTS.md` with non-obvious context for AI sessions
- `FEATURES.md` with honest feature inventory
- `TODO_LIST.md` with verified short-term work items
- `ROADMAP.md` with long-term direction
