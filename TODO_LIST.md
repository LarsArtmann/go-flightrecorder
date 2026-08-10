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

| Task                                         | Status    | Impact | Effort | Evidence                                                                              |
| -------------------------------------------- | --------- | ------ | ------ | ------------------------------------------------------------------------------------- |
| Push `v0.1.0` tag to remote                  | 🔴 `TODO` | High   | 2min   | `git ls-remote origin --tags` is empty; local tag exists but consumers cannot pin yet |
| Re-tag `v0.1.0` at HEAD before pushing       | 🟡 `IN_PROG` | Med | 2min   | Local `v0.1.0` predates the `lazyFile.Close` idempotency fix; tag should ship the fix |

## Recently completed (verified)

These were previously listed as TODO and are now done:

- **Publish to GitHub** — repo is public and `master` is in sync with `origin` (`git ls-remote origin` matches `HEAD`).
- **Tag `v0.1.0` (local)** — `git tag` lists `v0.1.0`. Only the remote push remains (above).
- **Add edge case tests for `lazyFile`** — `TestRecorder_LazyFileCloseWithoutSnapshot` (close without write), `TestSnapshotError_FileCreationFailure` (create-error typed error), and `TestRecorder_CloseIdempotentAfterSnapshot` (close-after-snapshot) all pass.
- **Add `SECURITY.md`** — committed; points to GitHub private vulnerability reporting.
- **Fix `lazyFile.Close` idempotency bug** — second `Recorder.Close` after a snapshot no longer returns "file already closed"; logged in `CHANGELOG.md`.
