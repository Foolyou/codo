## Purpose

Define the streamed assistant turn contract for incremental REPL rendering, streamed tool-call assembly, and failure handling that preserves coherent in-memory session history.

## Requirements

### Requirement: Stream assistant text into the REPL
The system SHALL request streamed chat completions for assistant turns and SHALL render assistant text incrementally in the active REPL session as streamed deltas arrive.

#### Scenario: Stream a text-only turn
- **WHEN** an operator submits a user message that produces assistant text without tool calls
- **THEN** the runtime SHALL display assistant text incrementally before the completion stream ends and SHALL finish the turn only after the stream reaches its terminal marker

### Requirement: Accumulate streamed tool calls before execution
The system SHALL accumulate streamed `tool_calls` fragments into complete tool invocations before executing the existing audited tool loop.

#### Scenario: Assemble streamed tool-call arguments
- **WHEN** the streamed completion emits tool-call metadata or function arguments across multiple events
- **THEN** the runtime SHALL merge the fragments by tool index and identifier into complete tool-call payloads before executing any tool

#### Scenario: Continue the same turn after streamed tool execution
- **WHEN** a streamed turn completes with one or more assembled tool calls
- **THEN** the runtime SHALL execute the tools, append their `tool` messages, and continue the same user turn with another streamed completion request until a final assistant message is produced or the configured tool-call limit is reached

### Requirement: Fail incomplete streamed turns clearly
The system SHALL surface clear errors for malformed or incomplete streamed completion responses and SHALL avoid storing failed partial output as a completed assistant message in session history.

#### Scenario: Stream closes before completion
- **WHEN** the runtime loses the streamed response before receiving a terminal completion marker
- **THEN** the REPL SHALL report the turn failure and SHALL NOT commit the partial assistant output as a completed assistant message in the durable in-memory conversation history

#### Scenario: Receive malformed streamed event data
- **WHEN** the runtime receives a streamed event that cannot be decoded into the supported OpenAI-compatible chat-completions schema
- **THEN** the REPL SHALL stop the turn and show a descriptive error instead of hanging or silently accepting incomplete state
