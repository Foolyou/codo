package runtime

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunAssistantREPLSupportsHelpResetAndExit(t *testing.T) {
	var output strings.Builder
	opts := AssistantREPLOptions{
		SessionID:     "sess_test",
		Model:         "qwen-test",
		WorkspaceRoot: "/workspace",
	}

	err := runAssistantREPL(
		context.Background(),
		opts,
		strings.NewReader("/help\n/reset\n/exit\n"),
		&output,
		assistantDependencies{
			requestCompletion: func(context.Context, AssistantREPLOptions, []assistantMessage) (assistantMessage, error) {
				t.Fatal("requestCompletion should not be called for control commands")
				return assistantMessage{}, nil
			},
			executeBash: func(context.Context, BashExecutionRequest) (BashExecutionResult, error) {
				t.Fatal("executeBash should not be called for control commands")
				return BashExecutionResult{}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("runAssistantREPL: %v", err)
	}

	text := output.String()
	for _, snippet := range []string{
		"Assistant REPL started",
		"Commands:",
		"Session history cleared.",
		"Session ended.",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected output to contain %q, got %q", snippet, text)
		}
	}
}

func TestAssistantSessionRunsToolCallLoop(t *testing.T) {
	opts := AssistantREPLOptions{
		SessionID:       "sess_test",
		Model:           "qwen-test",
		WorkspaceRoot:   "/workspace",
		MaxToolCalls:    3,
		BashTimeout:     time.Second,
		BashOutputBytes: 1024,
	}

	callCount := 0
	session := newAssistantSession(opts, assistantDependencies{
		requestCompletion: func(_ context.Context, _ AssistantREPLOptions, messages []assistantMessage) (assistantMessage, error) {
			callCount++
			switch callCount {
			case 1:
				return assistantMessage{
					Role:    "assistant",
					Content: nil,
					ToolCalls: []assistantToolCall{
						{
							ID:   "call_1",
							Type: "function",
							Function: assistantFunctionToolCall{
								Name:      "bash",
								Arguments: `{"command":"pwd","workdir":"."}`,
							},
						},
					},
				}, nil
			case 2:
				if len(messages) != 4 {
					t.Fatalf("expected system, user, assistant, and tool messages before second completion, got %d", len(messages))
				}
				toolMessage := messages[3]
				if toolMessage.Role != "tool" {
					t.Fatalf("expected tool message, got %+v", toolMessage)
				}
				content, ok := toolMessage.Content.(string)
				if !ok {
					t.Fatalf("expected tool content string, got %T", toolMessage.Content)
				}
				for _, want := range []string{`"tool_name":"bash"`, `"exec_id":"exec_1"`, `"session_id":"sess_test"`} {
					if !strings.Contains(content, want) {
						t.Fatalf("expected tool content to contain %q, got %s", want, content)
					}
				}
				return assistantMessage{
					Role:    "assistant",
					Content: "All done",
				}, nil
			default:
				t.Fatalf("unexpected completion call %d", callCount)
				return assistantMessage{}, nil
			}
		},
		executeBash: func(_ context.Context, req BashExecutionRequest) (BashExecutionResult, error) {
			if req.Command != "pwd" {
				t.Fatalf("unexpected command: %q", req.Command)
			}
			if req.Workdir != "/workspace" {
				t.Fatalf("unexpected workdir: %q", req.Workdir)
			}
			return BashExecutionResult{
				ExecID:             "exec_1",
				RuntimeInstanceID:  "rtm_1",
				SessionID:          "sess_test",
				WorkspaceID:        "workspace",
				WorkspacePathLabel: "workspace",
				Command:            req.Command,
				CWD:                req.Workdir,
				ExitCode:           0,
				Stdout:             "/workspace\n",
				StdoutBytes:        11,
				StdoutSHA256:       "sha",
			}, nil
		},
	})

	reply, err := session.RunTurn(context.Background(), "where am I?")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if reply != "All done" {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if len(session.messages) != 5 {
		t.Fatalf("expected conversation history to include final assistant reply, got %d messages", len(session.messages))
	}
}

func TestAssistantSessionReturnsFailingToolResultForUnsupportedCalls(t *testing.T) {
	callCount := 0
	session := newAssistantSession(AssistantREPLOptions{
		SessionID:       "sess_test",
		Model:           "qwen-test",
		WorkspaceRoot:   "/workspace",
		MaxToolCalls:    3,
		BashTimeout:     time.Second,
		BashOutputBytes: 1024,
	}, assistantDependencies{
		requestCompletion: func(_ context.Context, _ AssistantREPLOptions, messages []assistantMessage) (assistantMessage, error) {
			callCount++
			switch callCount {
			case 1:
				return assistantMessage{
					ToolCalls: []assistantToolCall{
						{
							ID:   "call_1",
							Type: "function",
							Function: assistantFunctionToolCall{
								Name:      "nope",
								Arguments: `{}`,
							},
						},
					},
				}, nil
			case 2:
				toolMessage := messages[3]
				content, ok := toolMessage.Content.(string)
				if !ok {
					t.Fatalf("expected tool content string, got %T", toolMessage.Content)
				}
				if !strings.Contains(content, `unsupported tool`) || !strings.Contains(content, `nope`) {
					t.Fatalf("expected unsupported tool failure, got %s", content)
				}
				return assistantMessage{Content: "handled"}, nil
			default:
				t.Fatalf("unexpected completion call %d", callCount)
				return assistantMessage{}, nil
			}
		},
		executeBash: func(context.Context, BashExecutionRequest) (BashExecutionResult, error) {
			t.Fatal("executeBash should not be called for unsupported tools")
			return BashExecutionResult{}, nil
		},
	})

	reply, err := session.RunTurn(context.Background(), "try something")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if reply != "handled" {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

func TestAssistantSessionEnforcesToolCallLimit(t *testing.T) {
	session := newAssistantSession(AssistantREPLOptions{
		SessionID:       "sess_test",
		Model:           "qwen-test",
		WorkspaceRoot:   "/workspace",
		MaxToolCalls:    1,
		BashTimeout:     time.Second,
		BashOutputBytes: 1024,
	}, assistantDependencies{
		requestCompletion: func(context.Context, AssistantREPLOptions, []assistantMessage) (assistantMessage, error) {
			return assistantMessage{
				ToolCalls: []assistantToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: assistantFunctionToolCall{
							Name:      "bash",
							Arguments: `{"command":"pwd"}`,
						},
					},
				},
			}, nil
		},
		executeBash: func(_ context.Context, req BashExecutionRequest) (BashExecutionResult, error) {
			return BashExecutionResult{
				ExecID:    "exec_1",
				SessionID: "sess_test",
				Command:   req.Command,
				CWD:       "/workspace",
				ExitCode:  0,
			}, nil
		},
	})

	reply, err := session.RunTurn(context.Background(), "loop")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if !strings.Contains(reply, "tool-call limit exceeded") {
		t.Fatalf("expected limit error, got %q", reply)
	}
}

func TestAssistantSessionResetStartsFreshConversation(t *testing.T) {
	callCount := 0
	session := newAssistantSession(AssistantREPLOptions{
		SessionID:       "sess_test",
		Model:           "qwen-test",
		WorkspaceRoot:   "/workspace",
		MaxToolCalls:    2,
		BashTimeout:     time.Second,
		BashOutputBytes: 1024,
	}, assistantDependencies{
		requestCompletion: func(_ context.Context, _ AssistantREPLOptions, messages []assistantMessage) (assistantMessage, error) {
			callCount++
			if callCount == 2 && len(messages) != 2 {
				t.Fatalf("expected reset conversation to include only system and user messages, got %d", len(messages))
			}
			return assistantMessage{Content: "ok"}, nil
		},
		executeBash: func(context.Context, BashExecutionRequest) (BashExecutionResult, error) {
			return BashExecutionResult{}, nil
		},
	})

	if _, err := session.RunTurn(context.Background(), "first"); err != nil {
		t.Fatalf("first RunTurn: %v", err)
	}
	session.Reset()
	if _, err := session.RunTurn(context.Background(), "second"); err != nil {
		t.Fatalf("second RunTurn: %v", err)
	}
}

func TestResolveAssistantWorkdirRejectsWorkspaceEscapes(t *testing.T) {
	_, err := resolveAssistantWorkdir("/workspace", "../etc")
	if err == nil {
		t.Fatal("expected workspace escape to be rejected")
	}

	got, err := resolveAssistantWorkdir("/workspace", "subdir")
	if err != nil {
		t.Fatalf("resolveAssistantWorkdir: %v", err)
	}
	if got != "/workspace/subdir" {
		t.Fatalf("unexpected resolved workdir: %q", got)
	}
}
