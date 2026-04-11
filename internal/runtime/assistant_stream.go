package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

type assistantCompletionStream interface {
	Next() (assistantChatCompletionChunk, bool, error)
	Close() error
}

type assistantHTTPCompletionStream struct {
	body   io.ReadCloser
	reader *bufio.Reader
}

type assistantChatCompletionChunk struct {
	Choices []assistantChatCompletionChunkChoice `json:"choices"`
}

type assistantChatCompletionChunkChoice struct {
	Index        int                         `json:"index"`
	Delta        assistantChatCompletionDelta `json:"delta"`
	FinishReason string                      `json:"finish_reason"`
}

type assistantChatCompletionDelta struct {
	Role      string                   `json:"role,omitempty"`
	Content   any                      `json:"content,omitempty"`
	ToolCalls []assistantToolCallDelta `json:"tool_calls,omitempty"`
}

type assistantToolCallDelta struct {
	Index    *int                            `json:"index,omitempty"`
	ID       string                          `json:"id,omitempty"`
	Type     string                          `json:"type,omitempty"`
	Function assistantFunctionToolCallDelta  `json:"function,omitempty"`
}

type assistantFunctionToolCallDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type assistantCompletionStep struct {
	Content   string
	ToolCalls []assistantToolCall
}

type assistantToolCallAccumulator struct {
	byIndex map[int]*assistantPartialToolCall
}

type assistantPartialToolCall struct {
	ID        string
	Type      string
	Name      string
	Arguments strings.Builder
}

type assistantStreamRenderer struct {
	output   io.Writer
	rendered bool
	lineOpen bool
}

func newAssistantStreamRenderer(output io.Writer) *assistantStreamRenderer {
	return &assistantStreamRenderer{output: output}
}

func (r *assistantStreamRenderer) WriteText(text string) error {
	if r.output == nil || text == "" {
		return nil
	}
	if _, err := io.WriteString(r.output, text); err != nil {
		return err
	}
	r.rendered = true
	r.lineOpen = !strings.HasSuffix(text, "\n")
	return nil
}

func (r *assistantStreamRenderer) FinishLine() error {
	if r.output == nil || !r.lineOpen {
		return nil
	}
	if _, err := io.WriteString(r.output, "\n"); err != nil {
		return err
	}
	r.lineOpen = false
	return nil
}

func openAssistantCompletionStream(ctx context.Context, opts AssistantREPLOptions, messages []assistantMessage) (assistantCompletionStream, error) {
	payload, err := json.Marshal(assistantChatCompletionRequest{
		Model:      opts.Model,
		Messages:   messages,
		Tools:      []assistantToolDefinition{assistantBashToolDefinition()},
		ToolChoice: "auto",
		Stream:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal chat completion request: %w", err)
	}

	resp, err := ProxyStreamRequest(ctx, http.MethodPost, "/v1/chat/completions", payload)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		responseBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("read proxy error response: %w", readErr)
		}
		return nil, fmt.Errorf("proxy request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return &assistantHTTPCompletionStream{
		body:   resp.Body,
		reader: bufio.NewReader(resp.Body),
	}, nil
}

func (s *assistantHTTPCompletionStream) Next() (assistantChatCompletionChunk, bool, error) {
	data, err := s.readEventData()
	if err != nil {
		return assistantChatCompletionChunk{}, false, err
	}
	if data == "[DONE]" {
		return assistantChatCompletionChunk{}, true, nil
	}

	var chunk assistantChatCompletionChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return assistantChatCompletionChunk{}, false, fmt.Errorf("decode streamed completion event: %w", err)
	}
	return chunk, false, nil
}

func (s *assistantHTTPCompletionStream) Close() error {
	return s.body.Close()
}

func (s *assistantHTTPCompletionStream) readEventData() (string, error) {
	var dataLines []string
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("read streamed completion event: %w", err)
		}

		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			if len(dataLines) > 0 {
				return strings.Join(dataLines, "\n"), nil
			}
			if err == io.EOF {
				return "", io.EOF
			}
			continue
		}

		if strings.HasPrefix(trimmed, "data:") {
			value := strings.TrimPrefix(trimmed, "data:")
			dataLines = append(dataLines, strings.TrimPrefix(value, " "))
		}

		if err == io.EOF {
			if len(dataLines) == 0 {
				return "", io.EOF
			}
			return strings.Join(dataLines, "\n"), nil
		}
	}
}

func consumeAssistantCompletionStream(stream assistantCompletionStream, renderer *assistantStreamRenderer) (assistantCompletionStep, error) {
	var content strings.Builder
	accumulator := assistantToolCallAccumulator{byIndex: map[int]*assistantPartialToolCall{}}
	finishReason := ""

	for {
		chunk, done, err := stream.Next()
		if err != nil {
			if err == io.EOF {
				return assistantCompletionStep{}, fmt.Errorf("stream ended before completion marker")
			}
			return assistantCompletionStep{}, err
		}
		if done {
			break
		}

		choice, ok, err := primaryAssistantChoice(chunk)
		if err != nil {
			return assistantCompletionStep{}, err
		}
		if !ok {
			continue
		}

		deltaText := flattenAssistantContent(choice.Delta.Content)
		if deltaText != "" {
			if err := renderer.WriteText(deltaText); err != nil {
				return assistantCompletionStep{}, err
			}
			content.WriteString(deltaText)
		}

		for _, toolCall := range choice.Delta.ToolCalls {
			if err := accumulator.Append(toolCall); err != nil {
				return assistantCompletionStep{}, err
			}
		}

		if choice.FinishReason != "" {
			if finishReason != "" && finishReason != choice.FinishReason {
				return assistantCompletionStep{}, fmt.Errorf("stream emitted multiple finish reasons: %q then %q", finishReason, choice.FinishReason)
			}
			finishReason = choice.FinishReason
		}
	}

	if finishReason == "" {
		return assistantCompletionStep{}, fmt.Errorf("stream ended without finish reason")
	}

	toolCalls, err := accumulator.Finalize()
	if err != nil {
		return assistantCompletionStep{}, err
	}

	switch finishReason {
	case "stop":
		if len(toolCalls) != 0 {
			return assistantCompletionStep{}, fmt.Errorf("stream finished with stop but included tool calls")
		}
	case "tool_calls":
		if len(toolCalls) == 0 {
			return assistantCompletionStep{}, fmt.Errorf("stream finished with tool_calls but no tool calls were assembled")
		}
	default:
		return assistantCompletionStep{}, fmt.Errorf("unsupported streamed finish reason %q", finishReason)
	}

	return assistantCompletionStep{
		Content:   content.String(),
		ToolCalls: toolCalls,
	}, nil
}

func primaryAssistantChoice(chunk assistantChatCompletionChunk) (assistantChatCompletionChunkChoice, bool, error) {
	if len(chunk.Choices) == 0 {
		return assistantChatCompletionChunkChoice{}, false, nil
	}
	for _, choice := range chunk.Choices {
		if choice.Index == 0 {
			return choice, true, nil
		}
	}
	return assistantChatCompletionChunkChoice{}, false, fmt.Errorf("streamed completion chunk missing choice index 0")
}

func (a *assistantToolCallAccumulator) Append(delta assistantToolCallDelta) error {
	if delta.Index == nil {
		return fmt.Errorf("streamed tool call chunk missing index")
	}
	partial, ok := a.byIndex[*delta.Index]
	if !ok {
		partial = &assistantPartialToolCall{}
		a.byIndex[*delta.Index] = partial
	}

	if delta.ID != "" {
		partial.ID = delta.ID
	}
	if delta.Type != "" {
		partial.Type = delta.Type
	}
	if delta.Function.Name != "" {
		partial.Name = delta.Function.Name
	}
	if delta.Function.Arguments != "" {
		if _, err := partial.Arguments.WriteString(delta.Function.Arguments); err != nil {
			return fmt.Errorf("append streamed tool arguments: %w", err)
		}
	}
	return nil
}

func (a *assistantToolCallAccumulator) Finalize() ([]assistantToolCall, error) {
	if len(a.byIndex) == 0 {
		return nil, nil
	}

	indexes := make([]int, 0, len(a.byIndex))
	for index := range a.byIndex {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)

	toolCalls := make([]assistantToolCall, 0, len(indexes))
	for _, index := range indexes {
		partial := a.byIndex[index]
		if partial.ID == "" {
			return nil, fmt.Errorf("streamed tool call %d missing id", index)
		}
		if partial.Name == "" {
			return nil, fmt.Errorf("streamed tool call %d missing function name", index)
		}

		toolType := partial.Type
		if toolType == "" {
			toolType = "function"
		}

		toolCalls = append(toolCalls, assistantToolCall{
			ID:   partial.ID,
			Type: toolType,
			Function: assistantFunctionToolCall{
				Name:      partial.Name,
				Arguments: partial.Arguments.String(),
			},
		})
	}

	return toolCalls, nil
}
