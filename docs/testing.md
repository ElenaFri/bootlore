# BootLore — Testing & TDD

BootLore is developed **test-first**. Every behaviour is described by a failing
test before the implementation exists, following the red → green → refactor
cycle. This document defines the test taxonomy, where each kind of test lives,
and how to run them.

## Philosophy

Testability is an architectural principle (see `docs/architecture.md` §9): core
components must be independently testable using **recorded journal fixtures**,
never a live system. Tests therefore:

- are deterministic and run anywhere (no root, no journald, no network);
- treat original system messages as sacred — a test fails if source data is
  altered or lost during normalization;
- document intended behaviour (test names read as specifications).

## The TDD cycle

1. **Red** — write the smallest test that expresses the next behaviour and watch
   it fail (a compile error counts as red).
2. **Green** — write the minimum code to make it pass.
3. **Refactor** — clean up with the tests as a safety net.

```
make test        # fast unit loop while coding
make test-all    # full suite before pushing
```

## Test taxonomy

| Level | Build tag | Location | Scope | Speed |
|---|---|---|---|---|
| **Unit** | _(none)_ | next to the code, `*_test.go` | one package in isolation, no I/O | very fast |
| **Integration** | `integration` | `test/integration/` | several packages together against on-disk journal fixtures | fast |
| **System** | `system` | `test/system/` | builds the real `bootlore` binary and runs it as a process | slower |

Build tags keep the slower tiers out of the default `go test ./...` loop, so unit
tests stay fast. Directories whose files are all tag-gated are simply skipped by
an untagged build.

### Unit tests

Pure logic, no file system, no external processes. Examples:

- `internal/event` — the event model: priority mapping, source/interpretation
  separation, graceful handling of zero-value events.
- `internal/journal` — journal JSON parsing edge cases (byte-array messages,
  missing/invalid fields, field preservation) using in-memory readers.
- `internal/render` — LOG line formatting.

```
make test-unit        # go test -race ./...
```

### Documentation tests

`test/docs` keeps the documentation honest: it fails when the man page
(`man/bootlore.1`) or the README table drift away from the CLI — a missing
command section, an undocumented flag, non-ASCII roff, or any `groff -ww`
warning (skipped if groff is absent). When you add a command or flag, update
the man page and the README; the test will remind you if you forget.

### Integration tests

Exercise the collector → parser → model → render path across package boundaries,
reading a realistic `journalctl -o json` export from
`test/testdata/boot-journal.json`.

```
make test-integration # go test -race -tags=integration ./test/integration/...
```

### System tests

Build the real binary once (`TestMain`) and drive the full CLI the way a user
would — via both `--input` and a stdin pipe — asserting stdout, stderr and exit
codes.

```
make test-system      # go test -tags=system ./test/system/...
```

## Fixtures

Recorded journal data lives under `test/testdata/` and package-local `testdata/`
directories. To capture a new fixture from a real machine:

```
journalctl -b -o json > test/testdata/boot-journal.json
```

Prefer small, curated fixtures that target a specific behaviour. Scrub anything
sensitive before committing (hostnames, IPs, user names).

## Conventions

- Name tests as specifications: `TestParseTolerates...`, `TestLogLineMissingFields`.
- One behaviour per test; use table-driven tests for input variations.
- Use `t.Helper()` in assertion helpers and `t.Cleanup()` for teardown.
- Keep source data immutable: when testing interpretation, assert the original
  `Message` is unchanged.
- Unit tests must not spawn external processes or touch the network; prefer
  in-memory inputs (use `t.TempDir` for the rare file-based case, e.g. `--input`).
- Share test doubles instead of copying them: the journal backend fakes
  (`FakeRunner`, `SequenceRunner`, `BlockingRunner`) and the `journalctl` stub
  helper live in `internal/collectortest` and are reused by the unit,
  integration and system tiers. It is imported only by tests and is excluded
  from the coverage report (see `codecov.yml`).

## Continuous integration

`.github/workflows/ci.yml` runs each tier as its own job (lint, unit,
integration, system) plus a build/smoke-test job, on every push to `main` and
every pull request. The Go version is pinned via `go-version-file: go.mod`.

A dedicated **coverage** job runs the unit + integration tiers with
`-coverpkg=./...`, which counts cross-package coverage: lines exercised by the
integration tier (e.g. the journalctl exec runner) therefore appear in the
profile that Codecov evaluates. The system tier runs the binary as a separate
process and is verified by its own job instead.

`make ci` reproduces the pipeline locally:

```
make ci   # lint + unit + integration + system
```

### Coverage badge (Codecov)

The `unit` job produces `coverage.out` and uploads it to
[Codecov](https://about.codecov.io/), which backs the coverage badge in the
README. The upload is **non-fatal** (`fail_ci_if_error: false`), so CI stays
green whether or not Codecov is wired up yet.

One-time setup to light up the badge:

1. Sign in to Codecov with GitHub and add the `ElenaFri/bootlore` repository.
2. Copy the repository upload token from the Codecov settings.
3. In GitHub: *Settings → Secrets and variables → Actions → New repository
   secret*, name it `CODECOV_TOKEN`, paste the value.

For a public repository the badge image works without a token; the token is only
required for the upload step to be reliable (and mandatory for private repos).
The badge markdown already lives at the top of `README.md`.

### Optional: golangci-lint

The pipeline intentionally depends only on the Go toolchain so it passes out of
the box. To add deeper static analysis locally or in CI:

```
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run
```
