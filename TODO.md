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
- [ ] Distinguish raw log severity from actual operational significance
- [ ] Identify warnings, failures and potentially relevant events
- [ ] Add contextual explanations for common boot messages
- [ ] Provide different levels of explanation: overview, technical and in-depth

## Phase 3 — CLI and TUI

- [ ] Implement the command-line interface
- [ ] Build a terminal-based real-time boot monitor
- [ ] Display a chronological event timeline
- [ ] Add event filtering and navigation
- [ ] Provide detailed event inspection
- [ ] Support small terminal sizes and monochrome output

## Phase 4 — Diagnostics and reporting

- [ ] Implement boot summary
- [ ] Identify failed and slow systemd services
- [ ] Provide boot duration and timing information
- [ ] Compare successive boot sessions
- [ ] Export diagnostic reports
- [ ] Add a system compatibility and permissions check (`doctor`)

## Phase 5 — System integration

- [ ] Implement an optional systemd service
- [ ] Run the collector under a dedicated unprivileged account
- [ ] Add optional access to the system journal
- [ ] Implement virtual terminal integration
- [ ] Handle unavailable or occupied virtual terminals gracefully
- [ ] Ensure BootLore never interferes with the normal boot process

## Phase 6 — Testing and distribution

- [ ] Test on different systemd-based Linux distributions
- [ ] Test under virtual machines and real hardware
- [ ] Test restricted permissions and failure scenarios
- [ ] Document architecture and design decisions
- [ ] Write installation and removal instructions
- [ ] Package for initial distribution

---

## Design principles

- **Read-only:** never modify system configuration or boot settings.
- **Non-intrusive:** never delay, block or interfere with system startup.
- **Unprivileged:** no root privileges required during normal operation.
- **Transparent:** always preserve access to original system messages.
- **Educational:** explain events without hiding technical details.
- **Resilient:** gracefully handle missing data, permissions and unsupported features.