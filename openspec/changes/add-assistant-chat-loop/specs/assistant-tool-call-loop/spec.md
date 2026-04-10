## ADDED Requirements

### Requirement: Proxy-mediated assistant turn execution
The system SHALL execute assistant turns from inside the runtime container by calling the configured host-side chat completions proxy with the active conversation state.

#### Scenario: Submit a user message
- **WHEN** an operator submits a message in the assistant REPL
- **THEN** the system SHALL issue a chat completions request through the configured host-side proxy from inside the runtime container using the active session context

#### Scenario: Preserve conversation within a session
- **WHEN** an operator sends multiple messages in the same REPL session
- **THEN** the system SHALL include prior user, assistant, and tool messages from that session in subsequent completion requests until the session is reset or exited

### Requirement: Standard `tool_calls` loop
The system SHALL use the OpenAI-compatible `tools` and `tool_calls` mechanism to execute assistant tools during a user turn.

#### Scenario: Model requests a registered tool
- **WHEN** the model returns a `tool_calls` entry for the registered `bash` tool
- **THEN** the system SHALL parse the function arguments, execute the tool, append the corresponding `tool` role message, and continue the same turn until it can produce a final assistant message

#### Scenario: Model returns an invalid tool request
- **WHEN** the model returns an unsupported tool name or malformed tool arguments
- **THEN** the system SHALL append a failing `tool` result that describes the error and SHALL continue the same turn without crashing the REPL

### Requirement: Audited `bash` tool execution
The system SHALL expose a minimal `bash` tool that executes inside the runtime through the existing audited bash execution path.

#### Scenario: Run bash in the workspace
- **WHEN** the assistant invokes the `bash` tool with a command and an optional working directory
- **THEN** the system SHALL execute the command inside the runtime container with a working directory resolved to the configured workspace mount path or one of its descendants

#### Scenario: Return bounded command results
- **WHEN** the `bash` tool completes, fails, or times out
- **THEN** the system SHALL return a structured tool result that includes execution status together with bounded stdout and stderr content and timeout or truncation indicators when applicable

### Requirement: Bounded per-turn tool execution
The system SHALL enforce a finite limit on tool-call iterations for a single user turn.

#### Scenario: Tool loop exceeds configured limit
- **WHEN** the model continues returning tool calls beyond the configured per-turn limit
- **THEN** the system SHALL stop the tool loop for that turn and surface a clear error instead of continuing indefinitely

#### Scenario: Reset starts a fresh turn context
- **WHEN** an operator resets the REPL session
- **THEN** the system SHALL discard prior conversation state so later turns no longer include earlier assistant or tool messages from that session
