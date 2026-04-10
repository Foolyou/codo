## ADDED Requirements

### Requirement: Container-local assistant REPL entrypoint
The system SHALL provide a container-local assistant command that starts an interactive REPL inside the long-lived runtime container.

#### Scenario: Start REPL inside runtime
- **WHEN** an operator invokes the assistant REPL command inside the runtime container
- **THEN** the system SHALL start an interactive assistant session without requiring the operator to manually wrap the command in a separate shell flow

#### Scenario: Exit REPL without stopping runtime
- **WHEN** an operator exits the assistant REPL
- **THEN** the system SHALL end the interactive session without stopping, rebuilding, or removing the long-lived runtime container

### Requirement: Host-side shortcut into the container REPL
The system SHALL provide a host-side assistant command that prepares the runtime dependencies and attaches the operator terminal to the container-local assistant REPL.

#### Scenario: Start assistant from host terminal
- **WHEN** an operator invokes the host-side assistant chat command
- **THEN** the system SHALL ensure the required control-plane services are available, ensure the runtime container is running, and attach the operator terminal to the assistant REPL inside that container

#### Scenario: Reuse healthy runtime services
- **WHEN** the configured control-plane sockets and runtime container are already healthy
- **THEN** the system SHALL reuse them instead of forcing a new runtime instance or rebuild before entering the assistant REPL

### Requirement: Basic REPL session controls
The system SHALL support line-oriented control commands for inspecting or resetting the current assistant session and for exiting the REPL.

#### Scenario: Show session help
- **WHEN** an operator enters the REPL help control command
- **THEN** the system SHALL display the supported REPL control commands without leaving the current session

#### Scenario: Reset session history
- **WHEN** an operator enters the REPL reset control command
- **THEN** the system SHALL clear the in-memory conversation history for the current session and continue accepting new input

#### Scenario: Exit session
- **WHEN** an operator enters the REPL exit control command
- **THEN** the system SHALL terminate the REPL session cleanly
