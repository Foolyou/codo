## Context

The current assistant path is intentionally non-streaming. The container-side turn engine in [`internal/runtime/assistant.go`](/home/chenan/projects/codo/internal/runtime/assistant.go) always sends `stream: false`, waits for a fully buffered chat-completions body, and only prints output after the full turn step completes. The host-side proxy already copies upstream bytes to the runtime, but the runtime-side proxy helper in [`internal/runtime/bash.go`](/home/chenan/projects/codo/internal/runtime/bash.go) reads the entire response into memory, and both the Unix-socket HTTP client and proxy upstream client use total-request timeouts that can cut off long-lived streamed responses.

The change needs to add streaming without weakening the current trust boundary. Model requests still need to originate inside the runtime container, upstream credentials still need to stay on the host, and audited `bash` execution still needs to remain the only v1 tool path. The design therefore has to cover transport, turn execution, and terminal rendering together.

## Goals / Non-Goals

**Goals:**
- Enable streamed chat-completions turns from the runtime to the proxy and from the proxy to the upstream provider.
- Render assistant text incrementally in the existing line-oriented REPL.
- Accumulate streamed `tool_calls` fragments into complete tool invocations and continue using the existing audited tool loop.
- Keep session history coherent so only completed assistant turn steps become durable conversation state.
- Extend tests and operator docs to cover the new streamed behavior and its failure modes.

**Non-Goals:**
- Replacing the REPL with a full-screen TUI or adding rich terminal layout.
- Adding new providers, new tools, transcript persistence, or reconnectable sessions.
- Introducing token-level audit persistence or host-side parsing of model semantics.
- Solving mid-turn cancellation and resume beyond what existing request-context cancellation already provides.

## Decisions

### 1. Keep the current line-oriented REPL and layer streaming onto it

The user-facing surface SHALL remain the existing plain REPL. Streaming SHALL only change how assistant output is produced: once the first text delta arrives, the REPL writes assistant text immediately and finishes the line when the streamed step ends. Tool-only streamed steps SHALL stay quiet until a tool result or final assistant text exists.

Why this approach:
- It improves responsiveness without forcing the project into a TUI redesign.
- It keeps the terminal contract stable for operators and tests.
- It preserves compatibility with the current slash-command REPL structure.

Alternatives considered:
- Move directly to a richer TUI: better presentation, but far more UI work than this change needs.
- Keep buffering and add only a spinner: simpler, but it does not provide actual streamed output.

### 2. Refactor the assistant turn engine around streamed completion events

The runtime SHALL replace the buffered `requestCompletion` step with a streaming completion path that can parse OpenAI-compatible SSE responses from `/v1/chat/completions`. The turn engine SHALL:
- send `stream: true` for assistant completion requests,
- iterate `data:` events until `[DONE]`,
- accumulate assistant text deltas in render order,
- accumulate streamed `tool_calls` fragments by tool index and identifier,
- finalize a turn step only when the stream reaches a terminal finish reason,
- append completed assistant and tool messages to working history using the same semantic order as the current non-streaming loop.

The parser SHALL treat malformed events, missing terminal markers, or incomplete tool-call payloads as turn failures instead of silently accepting partial state.

Why this approach:
- OpenAI-compatible SSE is the natural transport for streamed assistant output and streamed tool calls.
- An event-driven turn engine lets the runtime preserve existing tool-loop behavior while exposing text to the terminal as soon as it arrives.
- Keeping the parser inside the runtime preserves the architecture that the assistant, not the host proxy, owns conversation semantics.

Alternatives considered:
- Parse the stream on the host and forward a simplified protocol into the container: this would move assistant semantics out of the runtime boundary.
- Depend on a third-party SSE or provider SDK: unnecessary for a small, well-bounded protocol surface.

### 3. Treat the proxy as a transparent flush-through streaming boundary

The host-side model proxy SHALL continue to inject credentials and audit requests, but it SHALL not buffer streaming responses before returning data to the runtime. The proxy handler SHALL copy upstream streaming bytes to the runtime as they arrive and flush writable chunks whenever the response writer supports flushing. The runtime-side proxy client SHALL gain a streaming request helper that returns a live `http.Response` body instead of eagerly reading the full payload.

For streamed assistant turns, the proxy upstream client and Unix-socket HTTP client SHALL stop using whole-request timeouts that terminate long-lived streams mid-response. Stream lifetime SHALL instead be governed by the request context and ordinary connection failure behavior.

Why this approach:
- It keeps the credential and audit boundary intact while making streaming observable inside the container.
- It avoids duplicating parsing logic at the proxy layer.
- Removing whole-response timeouts eliminates an implementation detail that would otherwise break long streamed turns.

Alternatives considered:
- Keep current total-request timeouts: simpler, but incompatible with robust streaming.
- Buffer full upstream responses at the proxy and chunk them later: defeats the point of end-to-end streaming.

### 4. Only commit completed streamed steps to durable session history

Partial streamed text SHALL be rendered to the terminal immediately but buffered separately from the durable session transcript. On successful completion:
- a text-only step becomes one assistant message with the accumulated content,
- a tool-call step becomes one assistant message with accumulated content plus completed `tool_calls`, followed by the existing `tool` messages after execution.

If the stream fails before a terminal step is assembled, the runtime SHALL report the error and SHALL leave the durable conversation history unchanged for that failed step.

Why this approach:
- It preserves prompt integrity for later turns.
- It avoids contaminating session state with partially rendered or malformed assistant output.
- It keeps the tool loop semantics aligned with the existing message ordering.

Alternatives considered:
- Commit partial content continuously into session history: simpler to model, but brittle and hard to recover from on failure.

## Risks / Trade-offs

- [Users may see partial text before a later stream failure] → Report the failure clearly and do not commit the partial output into session history.
- [Streamed `tool_calls` fragments are easy to assemble incorrectly] → Restrict support to the OpenAI-compatible schema and add tests for multi-chunk arguments, mixed text-plus-tool streams, and malformed fragments.
- [Frequent flushing increases syscall overhead] → Flush at chunk or event boundaries rather than per byte or per rune.
- [Removing whole-request timeouts could expose longer waits on stalled upstreams] → Keep request-context cancellation as the governing control and document the current lack of a dedicated per-turn timeout as a future enhancement.

## Migration Plan

1. Add proxy and runtime transport helpers that can keep a streaming HTTP response body open across Unix sockets and verify that proxy audit records still persist after streamed responses finish.
2. Refactor the assistant turn engine to consume streamed completion events, render incremental text, accumulate tool calls, and preserve the existing per-turn tool budget and audited bash execution flow.
3. Update README guidance and automated tests for streamed text turns, streamed tool-call turns, malformed stream events, and proxy streaming behavior.

Rollback:
- Switch assistant turns back to `stream: false`.
- Restore buffered proxy-response handling in the runtime helper.
- Keep the existing REPL and tool loop behavior unchanged apart from removing the new streaming-specific code paths.

## Open Questions

- Should a later change add a dedicated per-turn model timeout now that whole-response transport timeouts are no longer appropriate for streaming?
- If the project eventually adopts a richer TUI, should it reuse the same streamed event engine or define a separate presentation-oriented abstraction on top of it?
