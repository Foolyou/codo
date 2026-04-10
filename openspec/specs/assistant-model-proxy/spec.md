## Purpose

Define the host-side model proxy contract that keeps provider credentials outside the assistant container while enforcing proxy-mediated upstream access and request auditability.

## Requirements

### Requirement: V1 supports Bailian pay-as-you-go APIs over the OpenAI-compatible interface
The system SHALL support Alibaba Cloud Bailian pay-as-you-go model APIs as the only upstream model provider in v1 and SHALL use that provider's OpenAI-compatible interface with host-managed provider-specific credentials and base URL configuration.

#### Scenario: Configure Bailian pay-as-you-go provider
- **WHEN** an operator configures the v1 model proxy
- **THEN** the system SHALL accept configuration for Alibaba Cloud Bailian's pay-as-you-go OpenAI-compatible integration path as the supported upstream provider target

#### Scenario: Reject unsupported upstream providers in v1
- **WHEN** an operator attempts to configure a different upstream provider or a non-OpenAI-compatible protocol for v1
- **THEN** the system SHALL reject that configuration as unsupported

### Requirement: Host-side credential injection for model requests
The system SHALL send assistant model requests from the container to a host-side AI proxy, and the proxy SHALL inject the upstream authentication credential on the host before forwarding the request to the real AI service.

#### Scenario: Forward request through proxy
- **WHEN** the assistant runtime issues a model request
- **THEN** the request SHALL be sent to the configured host-side AI proxy instead of directly to the upstream AI service

#### Scenario: Keep upstream credential outside container
- **WHEN** the host-side AI proxy forwards a model request upstream
- **THEN** the upstream authentication credential SHALL be added on the host side and SHALL NOT need to exist inside the assistant container

### Requirement: Proxy audit metadata
The host-side AI proxy SHALL persist an audit record for each forwarded model request with enough metadata to correlate usage to a runtime and workspace context.

#### Scenario: Record proxied request metadata
- **WHEN** the host-side AI proxy handles a model request
- **THEN** the system SHALL persist request metadata including request time, target model or endpoint, response status, `request_id`, `runtime_instance_id`, `session_id`, and a workspace-correlatable identifier

### Requirement: Fail request when proxy is unavailable
The assistant runtime SHALL depend on the configured host-side AI proxy for upstream model access.

#### Scenario: Proxy unavailable
- **WHEN** the assistant runtime cannot reach the configured host-side AI proxy
- **THEN** the system SHALL fail the model request rather than bypassing the proxy and contacting the upstream AI service directly

### Requirement: Future provider expansion does not break runtime trust boundaries
The system SHALL preserve the runtime-to-proxy trust boundary so that future versions can add additional upstream providers or protocol families without requiring upstream credentials to be moved into the assistant container.

#### Scenario: Add future upstream integration
- **WHEN** a future version adds another model provider or another protocol family
- **THEN** the system SHALL continue to keep upstream credentials host-managed and SHALL continue routing model access through the host-side proxy boundary
