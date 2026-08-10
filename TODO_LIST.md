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
| Create `flake.nix`              | 🔴 `TODO` | High   | 30min  | Lars's convention: Nix for ALL build/task automation; none exists |
| Tag `v0.1.0`                    | 🔴 `TODO` | High   | 5min   | `git tag` returns empty; blocks consumer version pinning     |
| Publish to GitHub               | 🔴 `TODO` | High   | 10min  | `git remote -v` returns empty; repo is local only            |

## Medium Impact

| Task                                         | Status    | Impact | Effort | Evidence                                                                |
| -------------------------------------------- | --------- | ------ | ------ | ----------------------------------------------------------------------- |
| Add GitHub Actions CI                        | 🔴 `TODO` | Med    | 30min  | No `.github/` dir; tests are local-only                                 |
| Fix errcheck warnings on `r.Start()` in tests | 🔴 `TODO` | Med    | 10min  | `recorder_test.go:90,137,173` — 3 unchecked Start returns (golangci-lint) |

## Low Impact

| Task                            | Status    | Impact | Effort | Evidence                          |
| ------------------------------- | --------- | ------ | ------ | --------------------------------- |
| Add `.editorconfig`             | 🔴 `TODO` | Low    | 5min   | Missing; editor consistency        |
| Add `.gitattributes`            | 🔴 `TODO` | Low    | 5min   | Missing; line-ending normalization |
| Add `CONTRIBUTING.md`           | 🔴 `TODO` | Low    | 15min  | Missing; open-source onboarding    |
