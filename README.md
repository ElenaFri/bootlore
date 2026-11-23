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
make build
```

### Commands

| Command | What it does |
|---|---|
| `bootlore collect` | Print a boot's events from journald, a captured export or stdin |
| `bootlore live` | Replay the current boot, then follow new events live (Ctrl-C stops) |
| `bootlore timeline` | Chronological timeline with boot-relative offsets (`+1.200s`) |
| `bootlore summary` | One-screen overview: counts, warnings, failures, timing, subsystems |
| `bootlore services` | Failed systemd services and reconstructed startup timings |
| `bootlore timing` | Boot duration: systemd's startup summary, phases and milestones |
| `bootlore compare` | Contrast two boots: duration (broken down per phase), counts and service-failure changes |
| `bootlore report` | Export a boot's full diagnosis (summary + timing + services) as text, markdown or JSON |
| `bootlore doctor` | Check system compatibility and journal permissions (exit 1 if a check fails) |
| `bootlore explain` | Events with educational explanations at three depths |
| `bootlore errors` | Only warnings and failures — what went wrong |
| `bootlore tui` | Interactive monitor: views, navigation, filtering, detail (needs a tty) |

Most commands accept the same sources — `--input FILE` (a `journalctl -o json`
export), piped stdin (`journalctl -b -o json | bootlore collect`), or the system
journal directly (the default in a terminal; `--journal` forces it) — and the
same views: `--view log` (faithful original messages, by default) or
`--view human` (BootLore's interpretation). `--category network,storage` keeps
only the events of the given subsystems (`kernel`, `hardware`, `storage`,
`network`, `systemd`, `security`, `desktop`, `other`, or `unclassified` to see
what BootLore could not classify); it composes with every command, e.g. with
`errors`.

Reading the system journal requires permission: if your account is not in the
`systemd-journal` group, `journalctl` shows only your own user-session journal.
BootLore then sees no system boot, so `bootlore timing` cannot measure it and
says so. Join the group with `sudo usermod -aG systemd-journal $USER` (then log
back in), or pipe the system journal once with
`sudo journalctl -b -o json | bootlore timing`.

### Optional systemd service

`bootlore tui --vt N` implements the *service mode*: the terminal is assigned
by systemd (`TTYPath`) and verified by BootLore — it never seizes a terminal,
never touches the console you are looking at, and exits gracefully when the
requested terminal is occupied or unavailable.

The optional unit `systemd/bootlore.service` starts the TUI on `tty9` at boot,
under a dedicated unprivileged account, without ever delaying the boot (it is
ordered after `systemd-journald`, requires nothing, opt-in via `systemctl
enable`). Installation as root:

```sh
useradd --system --home-dir /var/lib/bootlore --create-home --shell /usr/sbin/nologin bootlore
make build && install -Dm755 bin/bootlore /usr/bin/bootlore && install -Dm644 systemd/bootlore.service /etc/systemd/system/bootlore.service
systemctl daemon-reload && systemctl enable bootlore.service
```

The account gets read-only journal access through the standard `systemd-journal`
group (the unit adds it as a supplementary group); it needs no other privilege.
To use another terminal, override both values with a drop-in
(`systemctl edit bootlore.service`: `TTYPath=/dev/tty8` and `--vt 8`).

```sh
# Read the current boot straight from the system journal (LOG view)
./bin/bootlore collect

# Same boot, as readable descriptions (HUMAN view)
./bin/bootlore collect --view human

# Follow the boot live
./bin/bootlore live

# Timeline with offsets since boot
./bin/bootlore timeline

# Educational explanations, at three depths
./bin/bootlore explain --level in-depth

# Focus on what went wrong: warnings and failures only
./bin/bootlore errors

# ...narrowed to a subsystem
./bin/bootlore errors --category storage,hardware

# Previous boot, raw messages
./bin/bootlore collect --boot -1

# Compare the current boot against the previous one
# (duration broken down per phase, so a firmware/loader slowdown
#  is never mistaken for a Linux regression)
./bin/bootlore compare

# Export the current boot's full diagnosis for a bug report or tooling
./bin/bootlore report --format markdown
./bin/bootlore report --format json --output boot-report.json

# Check whether bootlore can read the journal on this machine
./bin/bootlore doctor
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
