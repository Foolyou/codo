package protocol

import "time"

type BashStartEvent struct {
	ExecID             string    `json:"exec_id"`
	RuntimeInstanceID  string    `json:"runtime_instance_id"`
	SessionID          string    `json:"session_id"`
	ContainerID        string    `json:"container_id"`
	WorkspaceID        string    `json:"workspace_id"`
	WorkspacePathLabel string    `json:"workspace_path_label"`
	Command            string    `json:"command"`
	CWD                string    `json:"cwd"`
	StartedAt          time.Time `json:"started_at"`
}

type BashEndEvent struct {
	ExecID          string    `json:"exec_id"`
	EndedAt         time.Time `json:"ended_at"`
	ExitCode        int       `json:"exit_code"`
	StdoutPreview   string    `json:"stdout_preview"`
	StderrPreview   string    `json:"stderr_preview"`
	StdoutBytes     int64     `json:"stdout_bytes"`
	StderrBytes     int64     `json:"stderr_bytes"`
	StdoutSHA256    string    `json:"stdout_sha256"`
	StderrSHA256    string    `json:"stderr_sha256"`
	StdoutTruncated bool      `json:"stdout_truncated"`
	StderrTruncated bool      `json:"stderr_truncated"`
}

type BashRecord struct {
	ExecID             string    `json:"exec_id"`
	RuntimeInstanceID  string    `json:"runtime_instance_id"`
	SessionID          string    `json:"session_id"`
	ContainerID        string    `json:"container_id"`
	WorkspaceID        string    `json:"workspace_id"`
	WorkspacePathLabel string    `json:"workspace_path_label"`
	Command            string    `json:"command"`
	CWD                string    `json:"cwd"`
	StartedAt          time.Time `json:"started_at"`
	EndedAt            time.Time `json:"ended_at"`
	ExitCode           int       `json:"exit_code"`
	StdoutPreview      string    `json:"stdout_preview"`
	StderrPreview      string    `json:"stderr_preview"`
	StdoutBytes        int64     `json:"stdout_bytes"`
	StderrBytes        int64     `json:"stderr_bytes"`
	StdoutSHA256       string    `json:"stdout_sha256"`
	StderrSHA256       string    `json:"stderr_sha256"`
	StdoutTruncated    bool      `json:"stdout_truncated"`
	StderrTruncated    bool      `json:"stderr_truncated"`
}

type ProxyAuditRecord struct {
	RequestID         string    `json:"request_id"`
	RequestTime       time.Time `json:"request_time"`
	RuntimeInstanceID string    `json:"runtime_instance_id"`
	SessionID         string    `json:"session_id"`
	WorkspaceID       string    `json:"workspace_id"`
	Method            string    `json:"method"`
	Path              string    `json:"path"`
	Query             string    `json:"query,omitempty"`
	ProviderType      string    `json:"provider_type"`
	TargetModel       string    `json:"target_model,omitempty"`
	ResponseStatus    int       `json:"response_status"`
	DurationMillis    int64     `json:"duration_millis"`
}
