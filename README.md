# BootLore

[![CI](https://github.com/ElenaFri/bootlore/actions/workflows/ci.yml/badge.svg)](https://github.com/ElenaFri/bootlore/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/ElenaFri/bootlore/branch/main/graph/badge.svg)](https://codecov.io/gh/ElenaFri/bootlore)

A Linux boot observer that turns system startup logs into a readable, real-time timeline

> Early development

## Rationale

Linux already provides powerful tools to inspect the boot process. `journalctl`, `dmesg` and `systemd-analyze` expose a wealth of information, but each presents a different piece of the puzzle: raw logs, kernel messages, service states or timing data.

BootLore aims to bring these pieces together. It turns boot-related events into a readable, chronological account of system startup, making it easier to understand not only *what* happened, but also *when* it happened and *why it matters*. Rather than replacing existing diagnostic tools, BootLore adds a layer of context, classification and explanation to make boot analysis more accessible without sacrificing technical detail, for both practical and learning purposes.

BootLore is designed to be lightweight, read-only and non-intrusive. It observes the system without changing its boot configuration or interfering with the startup process.

## Getting started

BootLore is written in [Go](https://go.dev) (see `go.mod` for the required version) and has no external dependencies.

```sh
# Build the CLI
make build

# Read a boot journal and print the LOG view
journalctl -b -o json | ./bin/bootlore collect

# ...or from a captured export
./bin/bootlore collect --input boot.json
```

## Development

The project is developed test-first (TDD) with three tiers of tests — unit,
integration and system. See [`docs/testing.md`](docs/testing.md) for the full
workflow.

```sh
make test        # fast unit loop
make test-all    # unit + integration + system
make ci          # everything CI runs (lint + all tests)
make help        # list all tasks
```
