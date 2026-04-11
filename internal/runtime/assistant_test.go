package runtime

import (
	"bufio"
	"context"
	"io"
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
			openCompletionStream: func(context.Context, AssistantREPLOptions, []assistantMessage) (assistantCompletionStream, error) {
				t.Fatal("openCompletionStream should not be called for control commands")
				return nil, nil
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

func TestAssistantSessionStreamsTextOnlyTurn(t *testing.T) {
	session := newAssistantSession(testAssistantOptions(), assistantDependencies{
		openCompletionStream: func(context.Context, AssistantREPLOptions, []assistantMessage) (assistantCompletionStream, error) {
			return fakeStream(
				streamChunk(textChoice("Hello", "")),
				streamChunk(textChoice(" world", "stop")),
				streamDone(),
			), nil
		},
		executeBash: func(context.Context, BashExecutionRequest) (BashExecutionResult, error) {
			t.Fatal("executeBash should not be called for text-only turn")
			return BashExecutionResult{}, nil
		},
	})

	var rendered strings.Builder
	reply, didRender, err := session.RunTurnStream(context.Background(), "hello", &rendered)
	if err != nil {
		t.Fatalf("RunTurnStream: %v", err)
	}
	if !didRender {
		t.Fatal("expected streamed output to be rendered")
	}
	if reply != "Hello world" {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if rendered.String() != "Hello world\n" {
		t.Fatalf("unexpected rendered output: %q", rendered.String())
	}
	if len(session.messages) != 3 {
		t.Fatalf("expected system, user, assistant messages, got %d", len(session.messages))
	}
}

func TestAssistantSessionRunsStreamedToolCallLoop(t *testing.T) {
	callCount := 0
	session := newAssistantSession(testAssistantOptions(), assistantDependencies{
		openCompletionStream: func(_ context.Context, _ AssistantREPLOptions, messages []assistantMessage) (assistantCompletionStream, error) {
			callCount++
			switch callCount {
			case 1:
				return fakeStream(
					streamChunk(toolChoice(
						"Inspecting",
						assistantToolCallDelta{
							Index: intPtr(0),
							ID:    "call_1",
							Type:  "function",
							Function: assistantFunctionToolCallDelta{
								Name:      "bash",
								Arguments: `{"command":"p`,
							},
						},
						"",
					)),
					streamChunk(toolChoice(
						"",
						assistantToolCallDelta{
							Index: intPtr(0),
							Function: assistantFunctionToolCallDelta{
								Arguments: `wd","workdir":"."}`,
							},
						},
						"tool_calls",
					)),
					streamDone(),
				), nil
			case 2:
				if len(messages) != 4 {
					t.Fatalf("expected system, user, assistant, and tool messages before second streamed completion, got %d", len(messages))
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
				return fakeStream(
					streamChunk(textChoice("All done", "stop")),
					streamDone(),
				), nil
			default:
				t.Fatalf("unexpected completion stream call %d", callCount)
				return nil, nil
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

	var rendered strings.Builder
	reply, didRender, err := session.RunTurnStream(context.Background(), "where am I?", &rendered)
	if err != nil {
		t.Fatalf("RunTurnStream: %v", err)
	}
	if !didRender {
		t.Fatal("expected streamed tool-call turn to render output")
	}
	if reply != "All done" {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if rendered.String() != "Inspecting\nAll done\n" {
		t.Fatalf("unexpected rendered output: %q", rendered.String())
	}
	if len(session.messages) != 5 {
		t.Fatalf("expected conversation history to include final assistant reply, got %d messages", len(session.messages))
	}
}

func TestAssistantSessionReturnsFailingToolResultForUnsupportedCalls(t *testing.T) {
	callCount := 0
	session := newAssistantSession(testAssistantOptions(), assistantDependencies{
		openCompletionStream: func(_ context.Context, _ AssistantREPLOptions, messages []assistantMessage) (assistantCompletionStream, error) {
			callCount++
			switch callCount {
			case 1:
				return fakeStream(
					streamChunk(toolChoice(
						"",
						assistantToolCallDelta{
							Index: intPtr(0),
							ID:    "call_1",
							Type:  "function",
							Function: assistantFunctionToolCallDelta{
								Name:      "nope",
								Arguments: `{}`,
							},
						},
						"tool_calls",
					)),
					streamDone(),
				), nil
			case 2:
				toolMessage := messages[3]
				content, ok := toolMessage.Content.(string)
				if !ok {
					t.Fatalf("expected tool content string, got %T", toolMessage.Content)
				}
				if !strings.Contains(content, `unsupported tool`) || !strings.Contains(content, `nope`) {
					t.Fatalf("expected unsupported tool failure, got %s", content)
				}
				return fakeStream(
					streamChunk(textChoice("handled", "stop")),
					streamDone(),
				), nil
			default:
				t.Fatalf("unexpected completion stream call %d", callCount)
				return nil, nil
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
	opts := testAssistantOptions()
	opts.MaxToolCalls = 1

	callCount := 0
	session := newAssistantSession(opts, assistantDependencies{
		openCompletionStream: func(context.Context, AssistantREPLOptions, []assistantMessage) (assistantCompletionStream, error) {
			callCount++
			return fakeStream(
				streamChunk(toolChoice(
					"",
					assistantToolCallDelta{
						Index: intPtr(0),
						ID:    "call_1",
						Type:  "function",
						Function: assistantFunctionToolCallDelta{
							Name:      "bash",
							Arguments: `{"command":"pwd"}`,
						},
					},
					"tool_calls",
				)),
				streamDone(),
			), nil
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
	if callCount != 2 {
		t.Fatalf("expected two streamed completion requests, got %d", callCount)
	}
}

func TestAssistantSessionResetStartsFreshConversation(t *testing.T) {
	callCount := 0
	session := newAssistantSession(testAssistantOptions(), assistantDependencies{
		openCompletionStream: func(_ context.Context, _ AssistantREPLOptions, messages []assistantMessage) (assistantCompletionStream, error) {
			callCount++
			if callCount == 2 && len(messages) != 2 {
				t.Fatalf("expected reset conversation to include only system and user messages, got %d", len(messages))
			}
			return fakeStream(
				streamChunk(textChoice("ok", "stop")),
				streamDone(),
			), nil
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

func TestAssistantSessionFailsMalformedStream(t *testing.T) {
	session := newAssistantSession(testAssistantOptions(), assistantDependencies{
		openCompletionStream: func(context.Context, AssistantREPLOptions, []assistantMessage) (assistantCompletionStream, error) {
			return httpCompletionStream("data: {not-json}\n\n"), nil
		},
		executeBash: func(context.Context, BashExecutionRequest) (BashExecutionResult, error) {
			t.Fatal("executeBash should not be called for malformed streams")
			return BashExecutionResult{}, nil
		},
	})

	var rendered strings.Builder
	_, didRender, err := session.RunTurnStream(context.Background(), "hello", &rendered)
	if err == nil {
		t.Fatal("expected malformed stream to fail")
	}
	if !strings.Contains(err.Error(), "decode streamed completion event") {
		t.Fatalf("unexpected error: %v", err)
	}
	if didRender {
		t.Fatal("did not expect malformed stream to render any output")
	}
	if rendered.Len() != 0 {
		t.Fatalf("expected no rendered output, got %q", rendered.String())
	}
	if len(session.messages) != 1 {
		t.Fatalf("expected failed turn to leave history unchanged, got %d messages", len(session.messages))
	}
}

func TestAssistantSessionFailsIncompleteStreamWithoutPersistingHistory(t *testing.T) {
	session := newAssistantSession(testAssistantOptions(), assistantDependencies{
		openCompletionStream: func(context.Context, AssistantREPLOptions, []assistantMessage) (assistantCompletionStream, error) {
			return httpCompletionStream("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Partial\"}}]}\n\n"), nil
		},
		executeBash: func(context.Context, BashExecutionRequest) (BashExecutionResult, error) {
			t.Fatal("executeBash should not be called for incomplete streams")
			return BashExecutionResult{}, nil
		},
	})

	var rendered strings.Builder
	_, didRender, err := session.RunTurnStream(context.Background(), "hello", &rendered)
	if err == nil {
		t.Fatal("expected incomplete stream to fail")
	}
	if !strings.Contains(err.Error(), "stream ended before completion marker") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !didRender {
		t.Fatal("expected partial streamed output to be rendered before failure")
	}
	if rendered.String() != "Partial\n" {
		t.Fatalf("unexpected rendered output: %q", rendered.String())
	}
	if len(session.messages) != 1 {
		t.Fatalf("expected failed turn to leave durable history unchanged, got %d messages", len(session.messages))
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

func testAssistantOptions() AssistantREPLOptions {
	return AssistantREPLOptions{
		SessionID:       "sess_test",
		Model:           "qwen-test",
		WorkspaceRoot:   "/workspace",
		MaxToolCalls:    3,
		BashTimeout:     time.Second,
		BashOutputBytes: 1024,
	}
}

type fakeCompletionStream struct {
	items []fakeCompletionStreamItem
	index int
}

type fakeCompletionStreamItem struct {
	chunk assistantChatCompletionChunk
	done  bool
	err   error
}

func fakeStream(items ...fakeCompletionStreamItem) assistantCompletionStream {
	return &fakeCompletionStream{items: items}
}

func (s *fakeCompletionStream) Next() (assistantChatCompletionChunk, bool, error) {
	if s.index >= len(s.items) {
		return assistantChatCompletionChunk{}, false, io.EOF
	}
	item := s.items[s.index]
	s.index++
	return item.chunk, item.done, item.err
}

func (s *fakeCompletionStream) Close() error {
	return nil
}

func streamChunk(choice assistantChatCompletionChunkChoice) fakeCompletionStreamItem {
	return fakeCompletionStreamItem{
		chunk: assistantChatCompletionChunk{
			Choices: []assistantChatCompletionChunkChoice{choice},
		},
	}
}

func streamDone() fakeCompletionStreamItem {
	return fakeCompletionStreamItem{done: true}
}

func textChoice(text string, finishReason string) assistantChatCompletionChunkChoice {
	return assistantChatCompletionChunkChoice{
		Index: 0,
		Delta: assistantChatCompletionDelta{
			Content: text,
		},
		FinishReason: finishReason,
	}
}

func toolChoice(content string, toolCall assistantToolCallDelta, finishReason string) assistantChatCompletionChunkChoice {
	return assistantChatCompletionChunkChoice{
		Index: 0,
		Delta: assistantChatCompletionDelta{
			Content:   content,
			ToolCalls: []assistantToolCallDelta{toolCall},
		},
		FinishReason: finishReason,
	}
}

func intPtr(value int) *int {
	return &value
}

func httpCompletionStream(body string) assistantCompletionStream {
	reader := strings.NewReader(body)
	readCloser := io.NopCloser(reader)
	return &assistantHTTPCompletionStream{
		body:   readCloser,
		reader: bufio.NewReader(reader),
	}
}
