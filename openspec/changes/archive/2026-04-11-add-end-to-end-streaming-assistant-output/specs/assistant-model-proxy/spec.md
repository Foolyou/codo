## ADDED Requirements

### Requirement: Streaming chat-completions passthrough
The host-side model proxy SHALL preserve streaming chat-completions responses between the runtime and the upstream provider without buffering the entire response body before forwarding incremental output to the runtime.

#### Scenario: Forward streaming completion chunks
- **WHEN** the assistant runtime sends a chat-completions request with streaming enabled
- **THEN** the proxy SHALL forward incremental response data to the runtime as upstream bytes arrive while preserving host-side credential injection and the existing proxy boundary

#### Scenario: Persist proxy audit after a streamed response
- **WHEN** a streamed chat-completions response completes or terminates with an upstream HTTP status
- **THEN** the proxy SHALL persist the normal request audit record after the streamed exchange ends without requiring the assistant runtime to contact the upstream provider directly
