## Context

The repository already has the infrastructure needed to host an assistant safely: a long-lived runtime container, a host-side model proxy that injects provider credentials, and an audited bash execution path. What is still missing is the assistant itself. There is no operator-facing assistant command, no conversation loop, and no runtime entrypoint that turns model responses into audited actions inside the container.

This change adds the first usable assistant interaction path. The user should be able to start from the host terminal, land in an assistant REPL that actually runs inside the runtime container, and let that REPL call the host-managed model proxy and the audited bash tool without widening the current trust boundary.

## Goals / Non-Goals

**Goals:**
- Provide a container-local `codo assistant repl` entrypoint that runs the assistant session inside the runtime container.
- Provide a host-side `codo assistant chat` command that gets the operator into that REPL without requiring a manual shell reconnect step.
- Implement a per-session chat loop using the OpenAI-compatible `/v1/chat/completions` API and the standard `tools` / `tool_calls` protocol.
- Support one minimal v1 tool, `bash`, and route it through the existing audited execution path.
- Keep session state, tool execution, and model traffic correlated by the existing runtime and session identifiers.
- Bound failure modes so malformed tool calls, large outputs, proxy failures, or runaway loops fail clearly.

**Non-Goals:**
- Full-screen TUI work, streaming token-by-token rendering, or rich terminal layout in v1.
- Persisted chat transcripts or reconnecting to prior assistant conversations.
- Additional tools beyond the initial `bash` tool.
- Multi-runtime orchestration, concurrent session management, or plugin-style tool registration.
- Changes to the provider boundary that would move upstream credentials into the runtime container.

## Decisions

### 1. Run the assistant loop inside the runtime container

The actual assistant REPL and turn engine SHALL run inside the long-lived runtime container. The host-side entrypoint SHALL only prepare access and attach the terminal to the in-container process.

Why this approach:
- It preserves the current trust boundary: model requests still originate from inside the runtime and reach the provider only through the mounted proxy socket.
- Tool execution stays inside the runtime container and naturally reuses the audited bash path.
- The same `codo` binary can expose host-side and container-side assistant commands without introducing a separate runtime helper binary.

Alternatives considered:
- Run the assistant loop on the host and treat the container as a pure tool target: simpler attachment logic, but it weakens the architecture by moving assistant state and model orchestration outside the runtime boundary.
- Require operators to reconnect to a shell manually and start the REPL themselves: lower implementation cost, but poor ergonomics and not the intended user flow.

### 2. Split the CLI into a host-side shortcut and a container-side REPL entrypoint

The CLI SHALL add an `assistant` command family with at least:
- `codo assistant chat`: host-side shortcut
- `codo assistant repl`: container-side REPL entrypoint

`codo assistant chat` SHALL ensure the runtime image and container are available, ensure the required control-plane sockets are usable, and then attach the operator terminal to `codo assistant repl` inside the container with a stable `session_id`.

The host-side command SHALL reuse an already healthy control plane when it can detect one from the configured Unix sockets. If the sockets are missing or unhealthy, it SHALL start a session-scoped control plane and tear it down when the REPL exits. This avoids requiring a separate foreground `codo up` process for the basic assistant flow while still allowing explicit operator-managed startup when desired.

Why this approach:
- It satisfies the requirement for a clear container-internal entrypoint while still giving the user a single host command to start chatting.
- It aligns with the existing long-lived runtime model: the container can persist across sessions even if the control plane is started on demand.
- It keeps `assistant chat` usable in the common case where the operator has not already started `codo up` in another terminal.

Alternatives considered:
- Make `assistant chat` depend on a separately running `codo up`: simpler lifecycle code, but awkward for the primary user path.
- Always start a fresh control plane and ignore existing sockets: easy to reason about, but risks colliding with an already running operator-managed control plane.

### 3. Use the standard OpenAI-compatible `tools` / `tool_calls` loop, not a custom textual ReAct protocol

The assistant turn engine SHALL call `/v1/chat/completions` through the existing proxy boundary and SHALL register tools using the standard `tools` field. When the model returns `tool_calls`, the runtime SHALL execute them, append `tool` role messages, and continue the same user turn until it can emit a final assistant message or a bounded failure.

The v1 loop SHALL be non-streaming. Each REPL turn waits for the completion response, executes any requested tools, and then prints the final assistant output for that turn.

Why this approach:
- `tool_calls` is the current interoperable contract for tool-using assistants and avoids inventing a new prompt-only protocol.
- The existing model proxy already forwards OpenAI-compatible requests, so this fits the current architecture.
- Non-streaming turns keep the first implementation focused on correctness of the tool loop and session state.

Alternatives considered:
- Prompt-only textual ReAct: easier to bootstrap, but less robust and harder to evolve once tool calling becomes real.
- Streaming from day one: better UX, but adds parsing and state complexity before the loop mechanics are proven.

### 4. Refactor audited bash execution into a reusable structured executor

The existing audited bash path currently targets terminal passthrough. The assistant tool loop needs the same audit guarantees, but it also needs structured results that can be sent back to the model. This change SHALL refactor the bash execution path so both the existing runtime command and the assistant tool can share one audited execution primitive.

The shared executor SHALL:
- post the existing start and end audit events
- execute the command with a bounded timeout
- capture bounded stdout and stderr for tool results
- return structured fields such as `exit_code`, `stdout`, `stderr`, truncation indicators, and timeout status

The assistant-facing `bash` tool SHALL accept a command string and an optional working directory, but the effective working directory SHALL resolve to the configured workspace mount path or one of its descendants. This keeps the tool aligned with the explicit-workspace model even though the container still contains other filesystem paths.

Why this approach:
- It reuses the existing audited shell path instead of creating a second shell execution mechanism.
- Structured output is required for the model loop and testability.
- Workspace-relative execution narrows the assistant's default operating area to the mounted project rather than the broader container filesystem.

Alternatives considered:
- Shell out to the existing `codo runtime bash` CLI command and parse terminal output: workable, but brittle and poorly structured.
- Execute bash directly without the audit wrapper for tool calls: simpler, but violates the audit guarantee already established for assistant shell access.

### 5. Keep the first interaction surface line-oriented and ephemeral

The REPL SHALL be a plain terminal loop with line-based input and a small set of slash commands such as `/help`, `/reset`, and `/exit`. Conversation history SHALL live only in memory for the lifetime of the REPL process.

Why this approach:
- It is enough to make the assistant usable from a terminal without committing to a more expensive TUI design.
- It keeps failure handling straightforward because each turn is request/response oriented.
- Ephemeral session state reduces scope while still allowing proxy and bash audits to correlate turns through `session_id`.

Alternatives considered:
- A full-screen TUI: richer UX, but unnecessary before the loop and tool mechanics are stable.
- Persisted transcript storage and session resume: useful later, but not required to prove the assistant path.

### 6. Bound per-turn and per-tool failure modes explicitly

The assistant loop SHALL enforce a finite tool-call budget for each user turn and SHALL return a clear error to the user when the model exceeds that budget. Individual `bash` calls SHALL also run under a timeout and return bounded output with truncation metadata when they exceed the capture limit.

Why this approach:
- Tool-calling models can loop or emit malformed calls; the runtime needs deterministic escape hatches.
- Bounded command output keeps prompts from ballooning and makes the REPL response size predictable.
- Clear error messages are better than silently hanging or crashing the whole session.

Alternatives considered:
- Unlimited tool loops and full captured output: easiest to implement, but too fragile for an operator-facing assistant.

## Risks / Trade-offs

- [Session-scoped control plane management adds lifecycle complexity] → Reuse existing healthy sockets when available and keep the fallback start/stop flow narrow.
- [The line-oriented REPL is less polished than a TUI] → Treat it as the stable v1 control surface and build richer UI later on top of the same assistant engine.
- [Bounded tool output may hide important command details] → Include truncation indicators in tool results and keep the command itself auditable on the host.
- [Workspace-scoped cwd does not stop commands from reading broader container paths] → Keep the container filesystem narrow and make the workspace boundary explicit in the assistant tool contract.
- [Non-streaming turns feel slower] → Prioritize correctness of the loop and defer streaming until the turn engine is stable.
- [Refactoring bash execution could regress the current runtime command path] → Preserve shared audit semantics and cover both terminal passthrough and structured tool execution with tests.

## Migration Plan

1. Add the new assistant CLI entrypoints without changing the existing `up`, `control-plane`, or `runtime` commands.
2. Refactor control-plane startup so `assistant chat` can reuse or temporarily host the services it depends on.
3. Implement the container-side assistant session engine and the proxy-backed chat-completions loop.
4. Refactor audited bash execution into a structured executor and expose it through the v1 `bash` tool.
5. Update operator documentation for the new assistant commands and session model.

Rollback:
- Stop using `codo assistant chat` and `codo assistant repl`.
- Keep the existing runtime, proxy, and audit commands as the fallback operational surface.
- Remove the assistant-specific code paths without changing the underlying runtime trust boundary.

## Open Questions

- Should a future version persist transcript history on the host, or should it remain purely in memory and rely on audit logs for post hoc correlation?
- When streaming is added later, should the final UI remain line-oriented or move directly to a richer TUI built on the same assistant engine?
