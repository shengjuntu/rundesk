package kun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	p "github.com/shengjuntu/rundesk/internal/kunproto"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const maxSSETokenSize = 2 << 20

type completion struct {
	Message p.Message
	Usage   json.RawMessage
}

func requestBody(s p.State) map[string]any {
	var definitions any = s.ToolDefinitions
	if len(s.ToolDefinitions) == 0 {
		definitions = toolDefinitions(s.Config.AllowWrite)
	}
	return map[string]any{"model": s.Config.Model, "messages": s.Messages, "tools": definitions, "stream": true}
}
func modelCall(ctx context.Context, s p.State, key string) (completion, error) {
	result := completion{Message: p.Message{Role: "assistant"}}
	body := p.JSON(requestBody(s))
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.Config.Endpoint, "/")+"/chat/completions", bytes.NewReader(body))
	if e != nil {
		return result, e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Timeout: time.Duration(s.Config.TimeoutSeconds) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, e := client.Do(req)
	if e != nil {
		return result, e
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return result, fmt.Errorf("model HTTP %d: %s", response.StatusCode, string(b))
	}
	// A hard bound prevents unbounded fragments even when a server never finishes.
	reader := io.LimitReader(response.Body, 8<<20)
	if strings.Contains(response.Header.Get("Content-Type"), "application/json") {
		var v struct {
			Choices []struct {
				Message p.Message `json:"message"`
				Finish  string    `json:"finish_reason"`
			} `json:"choices"`
			Usage json.RawMessage `json:"usage"`
			Error json.RawMessage `json:"error"`
		}
		if e = json.NewDecoder(reader).Decode(&v); e != nil {
			return result, e
		}
		if len(v.Choices) != 1 {
			return result, fmt.Errorf("model returned no single completion")
		}
		result.Message = v.Choices[0].Message
		result.Message.Role = "assistant"
		result.Usage = v.Usage
		if e = checkFinish(v.Choices[0].Finish); e != nil {
			return result, e
		}
		return result, validateCompletion(result)
	}
	decoder := newSSEDecoder(reader)
	calls := map[int]*p.ToolCall{}
	finished := false
	for decoder.Next() {
		data := decoder.Event().Data
		if data == "[DONE]" {
			break
		}
		var v struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int        `json:"index"`
						ID       string     `json:"id"`
						Function p.Function `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				Finish string `json:"finish_reason"`
			} `json:"choices"`
			Usage json.RawMessage `json:"usage"`
			Error json.RawMessage `json:"error"`
		}
		if e = json.Unmarshal([]byte(data), &v); e != nil {
			return result, fmt.Errorf("invalid model stream: %w", e)
		}
		if msg, ok := openAIStreamErrorMessage(v.Error); ok {
			return result, fmt.Errorf("model stream error: %s", msg)
		}
		if len(v.Usage) > 0 {
			result.Usage = v.Usage
		}
		if len(v.Choices) > 1 {
			return result, fmt.Errorf("multiple model choices unsupported")
		}
		for _, choice := range v.Choices {
			result.Message.Content += choice.Delta.Content
			for _, frag := range choice.Delta.ToolCalls {
				if frag.Index < 0 || frag.Index >= 32 {
					return result, fmt.Errorf("invalid tool index")
				}
				call := calls[frag.Index]
				if call == nil {
					call = &p.ToolCall{Type: "function"}
					calls[frag.Index] = call
				}
				call.ID += frag.ID
				call.Function.Name += frag.Function.Name
				call.Function.Arguments += frag.Function.Arguments
			}
			if choice.Finish != "" {
				if e = checkFinish(choice.Finish); e != nil {
					return result, e
				}
				finished = true
			}
		}
	}
	if decoder.Err() != nil {
		return result, decoder.Err()
	}
	if !finished {
		return result, fmt.Errorf("model stream ended without a complete finish_reason; no tools executed")
	}
	indices := []int{}
	for i := range calls {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	for _, i := range indices {
		result.Message.ToolCalls = append(result.Message.ToolCalls, *calls[i])
	}
	return result, validateCompletion(result)
}
func checkFinish(s string) error {
	if s != "stop" && s != "tool_calls" {
		return fmt.Errorf("incomplete or unsupported model finish_reason: %s", s)
	}
	return nil
}
func validateCompletion(c completion) error {
	if len(p.JSON(c)) > 1<<20 {
		return fmt.Errorf("model completion exceeds 1 MiB")
	}
	seen := map[string]bool{}
	if len(c.Message.ToolCalls) > 32 {
		return fmt.Errorf("too many tool calls")
	}
	for _, t := range c.Message.ToolCalls {
		if t.ID == "" || seen[t.ID] || len(t.ID) > 256 || t.Function.Name == "" || !json.Valid([]byte(t.Function.Arguments)) {
			return fmt.Errorf("invalid or duplicate tool call")
		}
		seen[t.ID] = true
	}
	if c.Message.Content == "" && len(c.Message.ToolCalls) == 0 {
		return fmt.Errorf("empty model completion")
	}
	return nil
}
