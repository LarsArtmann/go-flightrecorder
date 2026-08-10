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

| Task                            | Status    | Impact | Effort | Evidence                                                     |
| ------------------------------- | --------- | ------ | ------ | ------------------------------------------------------------ |
| Tag `v0.1.0`                    | 🔴 `TODO` | High   | 5min   | `git tag` returns empty; blocks consumer version pinning     |
| Publish to GitHub               | 🔴 `TODO` | High   | 10min  | `git remote -v` returns empty; repo is local only            |

## Medium Impact

| Task                                         | Status    | Impact | Effort | Evidence                                                                |
| -------------------------------------------- | --------- | ------ | ------ | ----------------------------------------------------------------------- |
| Add edge case tests for `lazyFile`           | 🔴 `TODO` | Med    | 10min  | No test for `Close()` when file was never opened                        |

## Low Impact

| Task                            | Status    | Impact | Effort | Evidence                          |
| ------------------------------- | --------- | ------ | ------ | --------------------------------- |
| Add `SECURITY.md`               | 🔴 `TODO` | Low    | 10min  | Links to GitHub advisories; needs remote first |
