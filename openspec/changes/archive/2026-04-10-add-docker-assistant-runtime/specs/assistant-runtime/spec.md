## ADDED Requirements

### Requirement: Long-lived rootless assistant runtime
The system SHALL run the assistant inside a rootless Docker container that is intended to remain available across user sessions until it is explicitly stopped, rebuilt, or removed.

#### Scenario: Start persistent runtime
- **WHEN** an operator starts the assistant runtime
- **THEN** the system SHALL create or start a rootless Docker container for the assistant instead of running the assistant process directly on the host

#### Scenario: Reuse existing runtime
- **WHEN** an operator reconnects to an already running assistant runtime
- **THEN** the system SHALL reuse the existing container instance rather than requiring a fresh container for every interaction

### Requirement: Explicit workspace mount
The system SHALL mount only the explicitly configured workspace directory into the assistant container and SHALL expose that workspace at a stable in-container path for assistant file operations.

#### Scenario: Mount selected workspace
- **WHEN** the assistant runtime is started with a configured workspace
- **THEN** the system SHALL make that workspace available inside the container at the configured stable mount path

#### Scenario: Exclude broad host mounts
- **WHEN** the assistant runtime is started
- **THEN** the system SHALL NOT mount the user's entire home directory or unrelated host configuration directories into the container as part of this change

### Requirement: Containerized bash tool access
The system SHALL provide the assistant with `bash` execution inside the container against the mounted workspace.

#### Scenario: Execute bash in container
- **WHEN** the assistant invokes the bash tool
- **THEN** the system SHALL execute the command inside the assistant container rather than on the host shell

### Requirement: Rebuildable runtime without embedded upstream credentials
The system SHALL support rebuilding or replacing the assistant container without requiring upstream AI credentials to be stored in the container image, container filesystem, or mounted workspace. For v1, the system SHALL treat container-internal state outside the mounted workspace as disposable and SHALL NOT require a separate long-lived runtime-state mount to restore normal operation after rebuild.

#### Scenario: Rebuild runtime
- **WHEN** an operator rebuilds or replaces the assistant container
- **THEN** the system SHALL be able to restore assistant operation using host-side configuration and the mounted workspace without recovering an upstream credential from inside the container

#### Scenario: Rebuild from clean container state
- **WHEN** an operator rebuilds or replaces the assistant container
- **THEN** the system SHALL resume operation without depending on previously persisted container-internal state outside the mounted workspace
