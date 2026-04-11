## 1. Streaming Transport and Proxy Path

- [x] 1.1 Add a runtime-side streaming proxy request helper and remove whole-response HTTP client timeouts that would terminate long-lived streamed turns
- [x] 1.2 Update the host-side model proxy to forward streaming chat-completions bytes with flush support while preserving existing credential injection and audit persistence
- [x] 1.3 Add transport and proxy tests that cover streaming passthrough behavior and audit-record persistence after streamed responses finish

## 2. Streamed Assistant Turn Engine

- [x] 2.1 Refactor the assistant completion path to request `stream: true` and parse OpenAI-compatible streamed chat-completions events inside the runtime
- [x] 2.2 Implement incremental REPL rendering for streamed assistant text while keeping the existing line-oriented command surface
- [x] 2.3 Accumulate streamed `tool_calls` fragments into complete tool invocations, execute them through the existing audited bash path, and preserve the configured per-turn tool-call limit
- [x] 2.4 Treat malformed or incomplete streamed responses as turn failures and keep failed partial output out of durable session history

## 3. Validation and Operator Docs

- [x] 3.1 Add runtime tests for streamed text-only turns, streamed tool-call turns, malformed streamed events, and incomplete stream failures
- [x] 3.2 Update README and operator-facing documentation to describe streamed assistant behavior, current limits, and the unchanged trust boundary
