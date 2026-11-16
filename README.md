# BootLore

A Linux boot observer that turns system startup logs into a readable, real-time timeline

> Early development

## Rationale

Linux already provides powerful tools to inspect the boot process. `journalctl`, `dmesg` and `systemd-analyze` expose a wealth of information, but each presents a different piece of the puzzle: raw logs, kernel messages, service states or timing data.

BootLore aims to bring these pieces together. It turns boot-related events into a readable, chronological account of system startup, making it easier to understand not only *what* happened, but also *when* it happened and *why it matters*. Rather than replacing existing diagnostic tools, BootLore adds a layer of context, classification and explanation to make boot analysis more accessible without sacrificing technical detail, for both practical and learning purposes.

BootLore is designed to be lightweight, read-only and non-intrusive. It observes the system without changing its boot configuration or interfering with the startup process.
