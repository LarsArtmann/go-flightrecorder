# Contributing

Thanks for your interest in contributing to `go-flightrecorder`!

## Prerequisites

- **Go 1.26** or later (`go.mod` pins `1.26.5`)
- **[golangci-lint](https://golangci-lint.run/)** v2.x (config is committed at
  [`.golangci.yml`](.golangci.yml))

There is no `justfile`, `Makefile`, or `flake.nix` — `go` and `golangci-lint`
are the only tools required.

## Development commands

```bash
go test ./... -race        # tests (always with -race)
golangci-lint run ./...    # lint (uses the committed .golangci.yml)
go vet ./...               # vet
```

### Coverage

`reports/` is gitignored (`.gitignore`). Regenerate coverage locally:

```bash
go test ./... -race -coverprofile=reports/coverage.out \
  && go tool cover -func=reports/coverage.out
```

## Constraints

### Process-global singleton

Go's `runtime/trace` allows only **one** active `FlightRecorder` per process.
Tests that call `Start`/`Stop` are serialized via `recorderMu` and intentionally
do **not** run in parallel (`paralleltest` is excluded for test files in
`.golangci.yml`).

### Zero dependencies

This library is stdlib-only. Do not add third-party imports.

## How to Contribute

1. Fork the repository
2. Create a feature branch from `master`
3. Make your changes with tests
4. Ensure all checks pass (`go test`, `golangci-lint run`, `go vet`)
5. Submit a pull request
