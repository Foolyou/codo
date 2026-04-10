## Purpose

Define the audited bash execution contract so every assistant shell command is mediated by a host-side audit path and persisted outside the container.

## Requirements

### Requirement: Audited bash execution wrapper
The system SHALL route every assistant bash invocation through a runtime wrapper that emits structured execution events to a host-side audit collector.

#### Scenario: Emit execution start event
- **WHEN** the assistant invokes the bash tool
- **THEN** the runtime SHALL send a start event to the host-side audit collector before executing the command

#### Scenario: Emit execution completion event
- **WHEN** a bash command finishes
- **THEN** the runtime SHALL send a completion event that includes the execution identifier and command result metadata to the host-side audit collector

### Requirement: Minimum bash audit fields
The system SHALL record each audited bash execution with a stable execution identifier, the executed command, working directory, timestamps, exit status, runtime correlation metadata sufficient to associate the command with a container instance and mounted workspace, and inline stdout/stderr preview metadata.

#### Scenario: Persist required audit fields
- **WHEN** the host-side audit collector stores a completed bash execution record
- **THEN** the persisted record SHALL include `exec_id`, `runtime_instance_id`, `session_id`, workspace correlation data, command, cwd, timing fields, exit code, `stdout_preview`, `stderr_preview`, byte counts, hashes, and truncation flags

### Requirement: Inline preview-based bash output audit
The system SHALL store bash stdout and stderr audit data inline in each execution record as bounded previews in v1, and SHALL mark whether either stream was truncated.

#### Scenario: Capture bounded command output previews
- **WHEN** a bash command produces stdout or stderr
- **THEN** the persisted audit record SHALL include inline previews for each stream together with truncation indicators and total byte counts

### Requirement: Fail closed when audit path is unavailable
The system SHALL refuse to execute assistant bash commands if the runtime cannot reach the host-side audit collector and establish the required audit event flow.

#### Scenario: Collector unavailable before execution
- **WHEN** the assistant invokes the bash tool and the host-side audit collector is unavailable
- **THEN** the system SHALL reject the command without executing it

### Requirement: Host-side audit persistence
The system SHALL persist assistant bash audit records outside the container so that audit history survives container restarts and rebuilds.

#### Scenario: Audit survives container replacement
- **WHEN** the assistant container is restarted or rebuilt after audited commands have already run
- **THEN** previously persisted bash audit records SHALL remain available on the host
