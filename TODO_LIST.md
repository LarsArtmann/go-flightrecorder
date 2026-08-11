# TODO List

> Short-term, actionable, bounded work items, verified against the actual code.
> For long-term vision and unrefined ideas, use ROADMAP.md.
> Items are ranked by impact. Status is verified, not assumed.

## Status legend

| Status           | Meaning                                                     |
| ---------------- | ----------------------------------------------------------- |
| 🔴 `TODO`        | Not started. Needs doing.                                   |
| 🟡 `IN_PROGRESS` | Actively being worked on.                                   |
| 🔵 `BLOCKED`     | Cannot proceed, external dependency or decision needed.     |
| 🟢 `DONE`        | Completed. Remove from this list and log in `CHANGELOG.md`. |

## High Impact

No high-impact tasks open. `v0.1.1` tagged and pushed; see `CHANGELOG.md`.
The operational feature set (compression, directory snapshots, retention,
async capture, observability hooks, nil-safe lifecycle) is implemented and
under test — see the `[Unreleased]` section of `CHANGELOG.md`.

## Recently completed (verified)

- **Operational features from real-world consumer feedback** — compression,
  snapshot-to-directory, retention, non-blocking capture, observability hooks,
  and nil-safe lifecycle. 58 tests pass with `-race`; lint clean.
- **Publish to GitHub** — repo is public and `master` is in sync with `origin`.
- **Tag and push `v0.1.0` + `v0.1.1`** — both tags on remote; consumers can pin versions.
- **Add edge case tests for `lazyFile`** — `TestRecorder_LazyFileCloseWithoutSnapshot`, `TestSnapshotError_FileCreationFailure`, `TestRecorder_CloseIdempotentAfterSnapshot` all pass.
- **Add `SECURITY.md`** — points to GitHub private vulnerability reporting.
- **Fix `lazyFile.Close` idempotency bug** — second `Recorder.Close` after a snapshot no longer returns "file already closed"; logged in `CHANGELOG.md`.
