# TODO

## Phase 1 — Core collector

- [x] Define the internal event data model
- [x] Collect boot events from `journald`
- [x] Support historical log replay and real-time event streaming
- [x] Preserve timestamps, priorities, sources and original messages
- [x] Handle missing permissions and unavailable log sources gracefully
- [x] Avoid duplicate or missing events during the transition from replay to live monitoring

## Phase 2 — Event processing

- [x] Parse and normalize collected events
- [x] Classify events by subsystem (kernel, hardware, storage, network, systemd, security, desktop)
- [x] Distinguish raw log severity from actual operational significance
- [x] Identify warnings, failures and potentially relevant events
- [x] Add contextual explanations for common boot messages
- [x] Provide different levels of explanation: overview, technical and in-depth

## Phase 3 — CLI and TUI

- [x] Implement the command-line interface (`collect`, `live`, `explain`, `errors`, with LOG/HUMAN views and explanation levels)
- [x] Build a terminal-based real-time boot monitor (`bootlore tui`: replay + live, LOG/HUMAN/EXPLAIN, navigation, filtering, monochrome-safe)
- [x] Display a chronological event timeline (`timeline`: monotonic offsets since boot, chronological order)
- [x] Add event filtering and navigation (`--category` on every command, `errors` for warnings/failures, in-session filtering/navigation in the TUI)
- [x] Provide detailed event inspection (`bootlore explain`; in the TUI: Enter opens the event detail with the three explanation levels)
- [x] Support small terminal sizes and monochrome output (no colours at all, reverse video only; explicit too-small message below 20x3)

## Phase 4 — Diagnostics and reporting

- [x] Implement boot summary
- [x] Identify failed and slow systemd services
- [x] Provide boot duration and timing information
- [x] Compare successive boot sessions
- [x] Export diagnostic reports
- [x] Add a system compatibility and permissions check (`doctor`)

## Phase 5 — System integration

- [x] Implement an optional systemd service (`systemd/bootlore.service`, opt-in only)
- [x] Run the collector under a dedicated unprivileged account (`User=bootlore`)
- [x] Add optional access to the system journal (`SupplementaryGroups=systemd-journal`)
- [x] Implement virtual terminal integration (`bootlore tui --vt N`; terminal assigned by systemd, verified by BootLore)
- [x] Handle unavailable or occupied virtual terminals gracefully (clear errors, exit code 1, `Restart=no`)
- [x] Ensure BootLore never interferes with the normal boot process (ordering only, no `Requires=`/`Before=`, guarded by `test/docs`)

## Phase 6 — Testing and distribution

- [ ] Test on different systemd-based Linux distributions
- [ ] Test under virtual machines and real hardware
- [ ] Test restricted permissions and failure scenarios
- [x] Document architecture and design decisions (`docs/architecture.md` reflects the implemented commands and service; guarded by test/docs)
- [x] Write installation and removal instructions (`make install`/`make uninstall`, DESTDIR/PREFIX aware; README incl. SELinux note; guarded by test/docs)
- [ ] Package for initial distribution

---

## Design principles

- **Read-only:** never modify system configuration or boot settings.
- **Non-intrusive:** never delay, block or interfere with system startup.
- **Unprivileged:** no root privileges required during normal operation.
- **Transparent:** always preserve access to original system messages.
- **Educational:** explain events without hiding technical details.
- **Resilient:** gracefully handle missing data, permissions and unsupported features.