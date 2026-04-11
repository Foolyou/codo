package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chenan/codo/internal/config"
	"github.com/chenan/codo/internal/ids"
)

const (
	DefaultAssistantModel         = "qwen-max"
	DefaultAssistantMaxToolCalls  = 8
	DefaultAssistantBashTimeout   = 30 * time.Second
	EnvAssistantModel             = "CODO_ASSISTANT_MODEL"
	DefaultAssistantInputMaxBytes = 1024 * 1024
)

type AssistantREPLOptions struct {
	SessionID       string
	Model           string
	WorkspaceRoot   string
	MaxToolCalls    int
	BashTimeout     time.Duration
	BashOutputBytes int
}

type assistantDependencies struct {
	openCompletionStream func(context.Context, AssistantREPLOptions, []assistantMessage) (assistantCompletionStream, error)
	executeBash          func(context.Context, BashExecutionRequest) (BashExecutionResult, error)
}

type assistantSession struct {
	opts     AssistantREPLOptions
	deps     assistantDependencies
	messages []assistantMessage
}

type assistantMessage struct {
	Role       string              `json:"role"`
	Content    any                 `json:"content"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	ToolCalls  []assistantToolCall `json:"tool_calls,omitempty"`
}

type assistantToolCall struct {
	ID       string                    `json:"id"`
	Type     string                    `json:"type"`
	Function assistantFunctionToolCall `json:"function"`
}

type assistantFunctionToolCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type assistantChatCompletionRequest struct {
	Model      string                    `json:"model"`
	Messages   []assistantMessage        `json:"messages"`
	Tools      []assistantToolDefinition `json:"tools"`
	ToolChoice string                    `json:"tool_choice,omitempty"`
	Stream     bool                      `json:"stream"`
}

type assistantToolDefinition struct {
	Type     string                        `json:"type"`
	Function assistantFunctionToolSpecBody `json:"function"`
}

type assistantFunctionToolSpecBody struct {
	Name        string                     `json:"name"`
	Description string                     `json:"description,omitempty"`
	Parameters  assistantToolJSONSchemaDef `json:"parameters"`
}

type assistantToolJSONSchemaDef struct {
	Type                 string                                 `json:"type"`
	Properties           map[string]assistantToolJSONSchemaProp `json:"properties"`
	Required             []string                               `json:"required,omitempty"`
	AdditionalProperties bool                                   `json:"additionalProperties"`
}

type assistantToolJSONSchemaProp struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type assistantBashToolArgs struct {
	Command string `json:"command"`
	Workdir string `json:"workdir,omitempty"`
}

type assistantToolResult struct {
	OK                 bool   `json:"ok"`
	ToolName           string `json:"tool_name"`
	ToolCallID         string `json:"tool_call_id,omitempty"`
	Error              string `json:"error,omitempty"`
	Command            string `json:"command,omitempty"`
	CWD                string `json:"cwd,omitempty"`
	ExecID             string `json:"exec_id,omitempty"`
	RuntimeInstanceID  string `json:"runtime_instance_id,omitempty"`
	SessionID          string `json:"session_id,omitempty"`
	WorkspaceID        string `json:"workspace_id,omitempty"`
	WorkspacePathLabel string `json:"workspace_path_label,omitempty"`
	ExitCode           int    `json:"exit_code"`
	Stdout             string `json:"stdout,omitempty"`
	Stderr             string `json:"stderr,omitempty"`
	StdoutBytes        int64  `json:"stdout_bytes"`
	StderrBytes        int64  `json:"stderr_bytes"`
	StdoutSHA256       string `json:"stdout_sha256,omitempty"`
	StderrSHA256       string `json:"stderr_sha256,omitempty"`
	StdoutTruncated    bool   `json:"stdout_truncated"`
	StderrTruncated    bool   `json:"stderr_truncated"`
	TimedOut           bool   `json:"timed_out"`
}

func RunAssistantREPL(ctx context.Context, opts AssistantREPLOptions) error {
	return runAssistantREPL(ctx, opts, os.Stdin, os.Stdout, defaultAssistantDependencies())
}

func AttachAssistantREPL(ctx context.Context, cfg config.Config, opts AssistantREPLOptions) error {
	normalized, err := normalizeAssistantREPLOptions(opts)
	if err != nil {
		return err
	}

	state, _, err := LoadOrCreateState(cfg.RuntimeStatePath(), cfg.Runtime.Name)
	if err != nil {
		return err
	}

	args := BuildDockerExecArgs(
		state.ContainerName,
		normalized.SessionID,
		cfg.Runtime.WorkspaceMountPath,
		buildAssistantReplCommand(normalized, cfg.Runtime.WorkspaceMountPath),
		true,
	)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("attach assistant repl inside runtime: %w", err)
	}
	return nil
}

func buildAssistantReplCommand(opts AssistantREPLOptions, workspaceRoot string) []string {
	command := []string{
		"codo",
		"assistant",
		"repl",
		"--workspace-root", workspaceRoot,
		"--model", opts.Model,
		"--max-tool-calls", strconv.Itoa(opts.MaxToolCalls),
		"--bash-timeout", opts.BashTimeout.String(),
		"--bash-output-bytes", strconv.Itoa(opts.BashOutputBytes),
	}
	if opts.SessionID != "" {
		command = append(command, "--session-id", opts.SessionID)
	}
	return command
}

func runAssistantREPL(ctx context.Context, opts AssistantREPLOptions, input io.Reader, output io.Writer, deps assistantDependencies) error {
	normalized, err := normalizeAssistantREPLOptions(opts)
	if err != nil {
		return err
	}
	if err := os.Setenv(EnvSessionID, normalized.SessionID); err != nil {
		return fmt.Errorf("set session env: %w", err)
	}
	if err := os.Setenv(EnvWorkspaceMountPath, normalized.WorkspaceRoot); err != nil {
		return fmt.Errorf("set workspace env: %w", err)
	}

	session := newAssistantSession(normalized, deps)
	fmt.Fprintf(output, "Assistant REPL started (session_id=%s, model=%s)\n", normalized.SessionID, normalized.Model)
	fmt.Fprintln(output, "Type /help for commands.")

	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 0, 1024), DefaultAssistantInputMaxBytes)
	for {
		fmt.Fprint(output, "codo> ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("read repl input: %w", err)
			}
			return nil
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		switch line {
		case "/help":
			writeAssistantHelp(output)
		case "/reset":
			session.Reset()
			fmt.Fprintln(output, "Session history cleared.")
		case "/exit":
			fmt.Fprintln(output, "Session ended.")
			return nil
		default:
			reply, rendered, err := session.RunTurnStream(ctx, line, output)
			if err != nil {
				fmt.Fprintf(output, "error: %v\n", err)
				continue
			}
			if rendered {
				continue
			}
			if strings.TrimSpace(reply) == "" {
				fmt.Fprintln(output, "(no assistant response)")
				continue
			}
			fmt.Fprintln(output, reply)
		}
	}
}

func newAssistantSession(opts AssistantREPLOptions, deps assistantDependencies) *assistantSession {
	session := &assistantSession{
		opts: opts,
		deps: deps,
	}
	session.Reset()
	return session
}

func (s *assistantSession) Reset() {
	s.messages = []assistantMessage{
		{
			Role:    "system",
			Content: assistantSystemPrompt(s.opts.WorkspaceRoot),
		},
	}
}

func (s *assistantSession) RunTurn(ctx context.Context, userInput string) (string, error) {
	reply, _, err := s.RunTurnStream(ctx, userInput, nil)
	return reply, err
}

func (s *assistantSession) RunTurnStream(ctx context.Context, userInput string, output io.Writer) (string, bool, error) {
	working := append([]assistantMessage(nil), s.messages...)
	working = append(working, assistantMessage{
		Role:    "user",
		Content: userInput,
	})
	renderer := newAssistantStreamRenderer(output)

	toolIterations := 0
	for {
		step, err := s.requestAssistantStep(ctx, working, renderer)
		if err != nil {
			_ = renderer.FinishLine()
			return "", renderer.rendered, err
		}

		if len(step.ToolCalls) == 0 {
			working = append(working, assistantMessage{
				Role:    "assistant",
				Content: step.Content,
			})
			s.messages = working
			if err := renderer.FinishLine(); err != nil {
				return "", renderer.rendered, err
			}
			return step.Content, renderer.rendered, nil
		}

		if toolIterations >= s.opts.MaxToolCalls {
			limitMessage := fmt.Sprintf("assistant tool-call limit exceeded after %d iterations", s.opts.MaxToolCalls)
			working = append(working, assistantMessage{
				Role:    "assistant",
				Content: limitMessage,
			})
			s.messages = working
			if err := renderer.FinishLine(); err != nil {
				return "", renderer.rendered, err
			}
			return limitMessage, renderer.rendered, nil
		}

		assistantEntry := assistantMessage{
			Role:      "assistant",
			Content:   nil,
			ToolCalls: step.ToolCalls,
		}
		if step.Content != "" {
			assistantEntry.Content = step.Content
		}
		working = append(working, assistantEntry)
		if err := renderer.FinishLine(); err != nil {
			return "", renderer.rendered, err
		}

		for _, toolCall := range step.ToolCalls {
			toolResult := s.executeToolCall(ctx, toolCall)
			working = append(working, assistantMessage{
				Role:       "tool",
				ToolCallID: toolCall.ID,
				Content:    toolResult,
			})
		}

		toolIterations++
	}
}

func (s *assistantSession) requestAssistantStep(ctx context.Context, messages []assistantMessage, renderer *assistantStreamRenderer) (assistantCompletionStep, error) {
	stream, err := s.deps.openCompletionStream(ctx, s.opts, messages)
	if err != nil {
		return assistantCompletionStep{}, err
	}
	defer stream.Close()

	return consumeAssistantCompletionStream(stream, renderer)
}

func (s *assistantSession) executeToolCall(ctx context.Context, toolCall assistantToolCall) string {
	if toolCall.Type != "" && toolCall.Type != "function" {
		return marshalAssistantToolResult(assistantToolResult{
			OK:         false,
			ToolName:   toolCall.Function.Name,
			ToolCallID: toolCall.ID,
			Error:      fmt.Sprintf("unsupported tool call type %q", toolCall.Type),
			SessionID:  s.opts.SessionID,
		})
	}

	switch toolCall.Function.Name {
	case "bash":
		return s.executeBashTool(ctx, toolCall)
	default:
		return marshalAssistantToolResult(assistantToolResult{
			OK:         false,
			ToolName:   toolCall.Function.Name,
			ToolCallID: toolCall.ID,
			Error:      fmt.Sprintf("unsupported tool %q", toolCall.Function.Name),
			SessionID:  s.opts.SessionID,
		})
	}
}

func (s *assistantSession) executeBashTool(ctx context.Context, toolCall assistantToolCall) string {
	var args assistantBashToolArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return marshalAssistantToolResult(assistantToolResult{
			OK:         false,
			ToolName:   toolCall.Function.Name,
			ToolCallID: toolCall.ID,
			Error:      fmt.Sprintf("decode bash tool arguments: %v", err),
			SessionID:  s.opts.SessionID,
		})
	}
	if strings.TrimSpace(args.Command) == "" {
		return marshalAssistantToolResult(assistantToolResult{
			OK:         false,
			ToolName:   toolCall.Function.Name,
			ToolCallID: toolCall.ID,
			Error:      "bash tool requires a non-empty command",
			SessionID:  s.opts.SessionID,
		})
	}

	workdir, err := resolveAssistantWorkdir(s.opts.WorkspaceRoot, args.Workdir)
	if err != nil {
		return marshalAssistantToolResult(assistantToolResult{
			OK:         false,
			ToolName:   toolCall.Function.Name,
			ToolCallID: toolCall.ID,
			Error:      err.Error(),
			Command:    args.Command,
			SessionID:  s.opts.SessionID,
		})
	}

	runCtx, cancel := context.WithTimeout(ctx, s.opts.BashTimeout)
	defer cancel()

	result, execErr := s.deps.executeBash(runCtx, BashExecutionRequest{
		Command:      args.Command,
		Workdir:      workdir,
		CaptureLimit: s.opts.BashOutputBytes,
	})
	if execErr != nil {
		return marshalAssistantToolResult(assistantToolResult{
			OK:                false,
			ToolName:          toolCall.Function.Name,
			ToolCallID:        toolCall.ID,
			Error:             execErr.Error(),
			Command:           args.Command,
			CWD:               workdir,
			RuntimeInstanceID: os.Getenv(EnvRuntimeInstanceID),
			SessionID:         s.opts.SessionID,
			WorkspaceID:       os.Getenv(EnvWorkspaceID),
		})
	}

	runError := ""
	if result.RunError != nil {
		runError = result.RunError.Error()
	}
	return marshalAssistantToolResult(assistantToolResult{
		OK:                 result.ExitCode == 0 && !result.TimedOut,
		ToolName:           toolCall.Function.Name,
		ToolCallID:         toolCall.ID,
		Error:              runError,
		Command:            args.Command,
		CWD:                result.CWD,
		ExecID:             result.ExecID,
		RuntimeInstanceID:  result.RuntimeInstanceID,
		SessionID:          result.SessionID,
		WorkspaceID:        result.WorkspaceID,
		WorkspacePathLabel: result.WorkspacePathLabel,
		ExitCode:           result.ExitCode,
		Stdout:             result.Stdout,
		Stderr:             result.Stderr,
		StdoutBytes:        result.StdoutBytes,
		StderrBytes:        result.StderrBytes,
		StdoutSHA256:       result.StdoutSHA256,
		StderrSHA256:       result.StderrSHA256,
		StdoutTruncated:    result.StdoutTruncated,
		StderrTruncated:    result.StderrTruncated,
		TimedOut:           result.TimedOut,
	})
}

func defaultAssistantDependencies() assistantDependencies {
	return assistantDependencies{
		openCompletionStream: openAssistantCompletionStream,
		executeBash:          ExecuteAuditedBash,
	}
}

func assistantBashToolDefinition() assistantToolDefinition {
	return assistantToolDefinition{
		Type: "function",
		Function: assistantFunctionToolSpecBody{
			Name:        "bash",
			Description: "Run a bash command inside the configured workspace and return bounded stdout and stderr output.",
			Parameters: assistantToolJSONSchemaDef{
				Type: "object",
				Properties: map[string]assistantToolJSONSchemaProp{
					"command": {
						Type:        "string",
						Description: "Bash command to execute.",
					},
					"workdir": {
						Type:        "string",
						Description: "Optional working directory relative to the workspace root or an absolute path under it.",
					},
				},
				Required:             []string{"command"},
				AdditionalProperties: false,
			},
		},
	}
}

func assistantSystemPrompt(workspaceRoot string) string {
	return fmt.Sprintf(
		"You are Codo, a coding assistant running inside a containerized workspace. "+
			"Use the bash tool when you need to inspect or modify files. "+
			"Keep commands focused, prefer working under %s, and explain results clearly when a command fails or output is truncated.",
		workspaceRoot,
	)
}

func writeAssistantHelp(output io.Writer) {
	fmt.Fprintln(output, "Commands:")
	fmt.Fprintln(output, "  /help  Show available REPL commands")
	fmt.Fprintln(output, "  /reset Clear in-memory conversation history for this session")
	fmt.Fprintln(output, "  /exit  End the REPL session")
}

func normalizeAssistantREPLOptions(opts AssistantREPLOptions) (AssistantREPLOptions, error) {
	normalized := opts
	if normalized.SessionID == "" {
		if current := os.Getenv(EnvSessionID); current != "" {
			normalized.SessionID = current
		} else {
			normalized.SessionID = ids.NewSessionID()
		}
	}
	if normalized.Model == "" {
		if envModel := os.Getenv(EnvAssistantModel); envModel != "" {
			normalized.Model = envModel
		} else {
			normalized.Model = DefaultAssistantModel
		}
	}
	if normalized.WorkspaceRoot == "" {
		if envRoot := os.Getenv(EnvWorkspaceMountPath); envRoot != "" {
			normalized.WorkspaceRoot = envRoot
		} else {
			normalized.WorkspaceRoot = config.DefaultWorkspaceMountPath
		}
	}
	if !filepath.IsAbs(normalized.WorkspaceRoot) {
		return AssistantREPLOptions{}, fmt.Errorf("assistant workspace root must be absolute: %q", normalized.WorkspaceRoot)
	}
	normalized.WorkspaceRoot = filepath.Clean(normalized.WorkspaceRoot)

	if normalized.MaxToolCalls <= 0 {
		normalized.MaxToolCalls = DefaultAssistantMaxToolCalls
	}
	if normalized.BashTimeout <= 0 {
		normalized.BashTimeout = DefaultAssistantBashTimeout
	}
	if normalized.BashOutputBytes <= 0 {
		normalized.BashOutputBytes = bashCaptureLimit()
	}
	return normalized, nil
}

func flattenAssistantContent(content any) string {
	switch value := content.(type) {
	case nil:
		return ""
	case string:
		return value
	case []any:
		var parts []string
		for _, entry := range value {
			part, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := part["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprint(value)
		}
		return string(encoded)
	}
}

func resolveAssistantWorkdir(workspaceRoot string, requested string) (string, error) {
	root := filepath.Clean(workspaceRoot)
	if requested == "" {
		return root, nil
	}

	var candidate string
	if filepath.IsAbs(requested) {
		candidate = filepath.Clean(requested)
	} else {
		candidate = filepath.Clean(filepath.Join(root, requested))
	}

	if candidate != root && !strings.HasPrefix(candidate, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("workdir %q must stay within workspace root %q", requested, root)
	}
	return candidate, nil
}

func marshalAssistantToolResult(result assistantToolResult) string {
	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf(`{"ok":false,"error":%q}`, err.Error())
	}
	return string(encoded)
}
