# BootLore — Architecture

> **Status:** Initial design  
> **Target platform:** Linux (systemd-based distributions)  
> **Implementation language:** Go

## 1. Purpose

BootLore is a Linux boot interpreter designed to make system startup understandable.

It collects boot-related events, presents them in real time and provides human-readable descriptions and contextual explanations.

Its purpose is not simply to display system logs in a more attractive format, but to help users understand what is happening during the boot process.

BootLore is built around three complementary capabilities:

- **Live monitoring:** observe system startup events as they occur.
- **Log / Human:** switch between original system messages and human-readable descriptions.
- **Educational explanations:** understand the underlying Linux mechanisms and their role in the boot process.

BootLore complements existing diagnostic tools such as `journalctl`, `dmesg` and `systemd-analyze`. It does not aim to replace them.

## 2. Core concepts

### 2.1 Log

The Log view displays the original system information.

It includes:

- Original journal message.
- Timestamp.
- Source and originating process.
- Journal priority.
- Relevant metadata.

Original messages must always remain accessible and unmodified.

### 2.2 Human

The Human view translates technical events into concise, understandable descriptions.

It answers:

- What just happened?
- Which system component is involved?
- What does this event mean in practical terms?

Descriptions must remain faithful to the original information.

BootLore must never present an inferred cause as an established fact.

### 2.3 Explain

The Explain view provides additional technical and educational context.

It answers:

- How does the underlying Linux mechanism work?
- Why does this event occur during startup?
- What is its relationship with other system components?
- What consequences might it have?
- What should be investigated if something goes wrong?

Explanations should be accessible to learners without becoming superficial for experienced administrators.

The three views are different representations of the same event, not independent event-processing pipelines.

## 3. Architectural overview

BootLore follows a modular architecture separating collection, event interpretation, contextual knowledge and presentation.

```text
                   Linux system
                        |
                        v
                 systemd-journald
                        |
                        v
                    Collector
                        |
                        v
                  Event parser
                        |
                        v
                    Event model
                        |
              +---------+---------+
              |                   |
              v                   v
          Classifier         Event context
              |                   |
              +---------+---------+
                        |
                        v
                 Event interpreter
                        |
              +---------+---------+
              |         |         |
              v         v         v
             LOG      HUMAN     EXPLAIN
              |         |         |
              +---------+---------+
                        |
                        v
                   Presentation
                    /       \
                   CLI       TUI
```

This diagram describes logical responsibilities rather than mandatory package boundaries.

## 4. Core components

### 4.1 Collector

The collector retrieves system events from available sources.

The initial implementation uses `systemd-journald`, accessed through `journalctl`.

Responsibilities:

- Retrieve events from the current boot session.
- Follow new events in real time.
- Support historical replay and live monitoring.
- Preserve original messages and metadata.
- Handle interruptions and unavailable sources.
- Avoid missing or duplicating events when transitioning from historical replay to live monitoring.

The collector must not modify system state.

The journal is the primary source of information for the initial implementation. Additional collection mechanisms may be considered later.

### 4.2 Event parser

The parser converts source-specific data into a normalized internal representation.

Each event should retain, where available:

- Wall-clock timestamp.
- Monotonic timestamp.
- Boot identifier.
- Source and originating process.
- Journal priority.
- Original message.
- Relevant journal metadata.
- Collection origin (historical or live).

The internal event model must remain independent of the journal output format.

Original source information must never be discarded during normalization.

### 4.3 Event model

The event model is the central data structure shared by all application components.

It separates:

- **Source data:** what the system actually reported.
- **Classification:** which subsystem is involved.
- **Human description:** a concise reformulation.
- **Educational explanation:** additional technical context.
- **Interpretation metadata:** confidence, provenance and availability.

An event must remain valid and displayable even if no classification, description or explanation is available.

Interpretations must never overwrite original source data.

### 4.4 Classifier

The classifier identifies the subsystem associated with an event.

Initial categories:

- `KERNEL`
- `HARDWARE`
- `STORAGE`
- `NETWORK`
- `SYSTEMD`
- `SECURITY`
- `DESKTOP`
- `OTHER`

Classification should rely on explicit information whenever possible, such as journal fields, unit names and message patterns.

Unknown or ambiguous events must remain unclassified rather than being assigned an unreliable category.

Journal priority and operational severity must be treated separately. A warning does not necessarily indicate a system failure.

### 4.5 Event interpreter

The interpreter produces the human-readable representation of an event.

It combines source information, classification and contextual knowledge to generate concise descriptions.

Its responsibilities include:

- Identify known event patterns.
- Generate faithful human-readable descriptions.
- Associate events with relevant technical concepts.
- Indicate uncertainty when interpretation is incomplete.
- Provide references to additional explanations.

The initial implementation should favour explicit, documented interpretation rules over opaque or speculative inference.

An event for which no reliable interpretation exists must fall back to its original message.

### 4.6 Knowledge base

The knowledge base contains educational explanations associated with Linux and system startup concepts.

It should cover topics such as:

- Kernel initialization.
- Initramfs and root filesystem mounting.
- systemd units and targets.
- Service dependencies and ordering.
- Hardware detection.
- Storage initialization.
- Network configuration.
- Graphical session startup.

Knowledge entries should be independently maintained, documented and testable.

Explanations must distinguish established technical facts from possible interpretations and diagnostic hypotheses.

The knowledge base is initially local and does not require an external service or network connection.

### 4.7 Timeline and event context

The timeline organizes events chronologically.

It supports:

- Historical boot reconstruction.
- Live event progression.
- Event navigation and filtering.
- Identification of related events.
- Display of relevant timing information.

Monotonic timestamps should be preferred for ordering events within a boot session when available.

Related events may be associated to provide additional context, but temporal proximity alone must not be treated as proof of causality.

### 4.8 Presentation

The presentation layer provides access to event information.

**CLI**

The command-line interface is the first implementation target.

Planned commands include:

- `bootlore collect`
- `bootlore live`
- `bootlore timeline`
- `bootlore explain`
- `bootlore services`
- `bootlore errors`

**TUI**

The terminal user interface will provide interactive live monitoring and historical inspection.

Its central feature is the ability to switch between:

- LOG: original system messages.
- HUMAN: readable event descriptions.
- EXPLAIN: educational technical context.

The interface should also support event navigation, filtering and detailed inspection.

Both interfaces must consume the same internal event model and interpretation logic.

## 5. Live monitoring and historical reconstruction

BootLore supports two complementary collection modes.

### Historical replay

BootLore retrieves events from the current boot session that remain available in the system journal.

These events are presented as historical information, not as events observed live by BootLore.

### Live monitoring

Once collection begins, BootLore follows newly recorded journal events and presents them as they arrive.

The transition between historical replay and live monitoring must be designed to avoid missing or duplicating events.

The application must clearly distinguish historical events from newly observed events.

### Observability limitations

BootLore cannot guarantee observation of the entire boot process from the first kernel instruction.

Its initial implementation depends on the availability of systemd-journald and the events retained in the journal.

Consequently:

- Some early kernel or initramfs events may be unavailable.
- Historical visibility depends on journal configuration and permissions.
- Live observation begins only when BootLore starts collecting events.

These limitations must be communicated clearly to the user.

## 6. Runtime modes

### Standalone mode

The user starts BootLore manually after system startup.

This is the primary development and diagnostic mode.

It allows retrospective inspection of the current boot and live monitoring of subsequent events.

### Automatic startup mode

An optional systemd service may start BootLore automatically during system startup.

The service is intended to:

- Run under a dedicated unprivileged account.
- Start after the journal is available.
- Use a configurable virtual terminal, where supported.
- Never block or delay normal system startup.
- Never take control of the active console automatically.
- Exit gracefully if the requested terminal is unavailable.

Virtual terminal integration is an optional deployment feature and must not be a dependency of the core application.

## 7. Permissions and security

BootLore follows the principle of least privilege.

Two access levels are envisaged:

- **Standard:** access to journal entries available to the current user.
- **Extended:** optional access to the system journal through appropriate group membership or another explicitly configured mechanism.

Extended journal access may expose sensitive system information and must be clearly documented.

Normal application execution must not require root privileges.

Administrative privileges may be required during installation to configure an optional service, create a dedicated account or establish access permissions.

BootLore must not modify:

- GRUB configuration.
- Initramfs images.
- Kernel parameters.
- System boot configuration.

## 8. Implementation strategy

Development should proceed incrementally.

| Stage | Objective |
|---|---|
| 1 | Collect journal events and normalize their data |
| 2 | Implement reliable historical replay and live monitoring |
| 3 | Define and validate the internal event model |
| 4 | Implement the LOG / HUMAN representation |
| 5 | Build the initial educational knowledge base |
| 6 | Add event classification and contextual relationships |
| 7 | Implement CLI commands and interactive TUI |
| 8 | Add optional systemd and virtual terminal integration |
| 9 | Test, document and package |

The first milestone is a working collector capable of retrieving the current boot journal, following new events and producing normalized events without loss or duplication.

The second milestone is a usable LOG / HUMAN interface.

## 9. Architectural principles

- **Faithfulness:** never alter or conceal original system messages.
- **Clarity:** make technical events understandable without oversimplifying them.
- **Transparency:** distinguish facts, interpretations and hypotheses.
- **Non-intrusiveness:** never interfere with normal system startup.
- **Least privilege:** use only the permissions strictly required.
- **Modularity:** keep collection, interpretation, knowledge and presentation independent.
- **Resilience:** handle missing permissions, unavailable sources and incomplete information gracefully.
- **Educational value:** explain underlying mechanisms, not merely individual messages.
- **Testability:** make core components independently testable using recorded journal fixtures.
