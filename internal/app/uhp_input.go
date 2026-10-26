package app

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"strings"
)

// Inline base64 is bounded independently of the upload endpoint. Large clients
// should upload once, then reference the file id rather than repeat the bytes.
const uhpRequestLimit = 8 << 20

type uhpRequest struct {
	Input                json.RawMessage `json:"input"`
	Metadata             map[string]any  `json:"metadata"`
	Model                string          `json:"model"`
	PreviousResponseID   string          `json:"previous_response_id"`
	Instructions         string          `json:"instructions"`
	Stream               bool            `json:"stream"`
	Background           bool            `json:"background"`
	Store                *bool           `json:"store"`
	MaxStep              *int            `json:"max_step"`
	TimeoutSeconds       *int            `json:"timeout_seconds"`
	MaxOutputTokens      *int            `json:"max_output_tokens"`
	IgnoredFields        []string        `json:"-"`
	InstructionMode      string          `json:"-"`
	RequiredCapabilities []string        `json:"-"`
}

func parseUHPRequest(reader io.Reader) (uhpRequest, error) {
	var out uhpRequest
	b, err := io.ReadAll(io.LimitReader(reader, uhpRequestLimit+1))
	if err != nil {
		return out, failure(400, "invalid_input", "The request body could not be read.")
	}
	if len(b) > uhpRequestLimit {
		return out, failure(413, "file_too_large", "The inline request exceeds the 8 MiB limit; use a file upload.")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(b, &fields) != nil || fields == nil {
		return out, failure(400, "invalid_input", "The request must be one JSON object.")
	}
	known := map[string]any{"input": &out.Input, "metadata": &out.Metadata, "model": &out.Model, "previous_response_id": &out.PreviousResponseID, "instructions": &out.Instructions, "stream": &out.Stream, "background": &out.Background, "store": &out.Store, "max_step": &out.MaxStep, "timeout_seconds": &out.TimeoutSeconds, "max_output_tokens": &out.MaxOutputTokens}
	for name, target := range known {
		if raw, ok := fields[name]; ok && json.Unmarshal(raw, target) != nil {
			return out, failure(400, "invalid_input", "A request field has the wrong type.")
		}
	}
	for name, raw := range fields {
		switch name {
		case "input", "model", "metadata", "instructions", "stream", "background", "store":
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return out, failure(400, "invalid_input", "A required field value cannot be null.")
			}
		case "previous_response_id", "timeout_seconds":
		case "max_step":
			if out.MaxStep != nil {
				out.IgnoredFields = append(out.IgnoredFields, name)
			}
		case "max_output_tokens":
			out.IgnoredFields = append(out.IgnoredFields, name)
		case "tools":
			var tools []map[string]any
			if json.Unmarshal(raw, &tools) != nil || tools == nil {
				return out, failure(400, "invalid_input", "tools must be an array of objects.")
			}
			for _, tool := range tools {
				if tool == nil {
					return out, failure(400, "invalid_input", "tools must be an array of objects.")
				}
			}
			out.IgnoredFields = append(out.IgnoredFields, name)
		case "include":
			var values []string
			if json.Unmarshal(raw, &values) != nil || values == nil {
				return out, failure(400, "invalid_input", "include must be an array of strings.")
			}
			out.IgnoredFields = append(out.IgnoredFields, name)
		default:
			out.IgnoredFields = append(out.IgnoredFields, name)
		}
	}
	if len(out.Input) == 0 {
		return out, failure(400, "invalid_input", "input is required.")
	}
	if out.Metadata == nil {
		out.Metadata = map[string]any{}
	}
	if id, ok := out.Metadata["harness_id"]; ok {
		if _, valid := id.(string); !valid {
			return out, failure(400, "invalid_input", "metadata.harness_id must be a string.")
		}
	}
	if raw, exists := out.Metadata["rundesk_require"]; exists {
		values, ok := raw.([]any)
		if !ok || len(values) > 16 {
			return out, failure(400, "invalid_input", "metadata.rundesk_require must be an array of at most 16 capability names.")
		}
		seen := map[string]bool{}
		for _, value := range values {
			name, ok := value.(string)
			if !ok || name == "" || len(name) > 64 {
				return out, failure(400, "invalid_input", "Required capability names must be non-empty strings of at most 64 bytes.")
			}
			if !seen[name] {
				out.RequiredCapabilities = append(out.RequiredCapabilities, name)
				seen[name] = true
			}
		}
		sort.Strings(out.RequiredCapabilities)
	}
	for _, budget := range []*int{out.MaxStep, out.TimeoutSeconds, out.MaxOutputTokens} {
		if budget != nil && *budget < 1 {
			return out, failure(400, "invalid_input", "Budgets must be positive integers.")
		}
	}
	if out.TimeoutSeconds != nil && *out.TimeoutSeconds > uhpMaximumTimeoutSeconds {
		return out, failure(400, "invalid_input", "timeout_seconds must not exceed 86400.")
	}
	if len(out.Model) > 160 || len(out.PreviousResponseID) > 160 || len(out.Instructions) > 64<<10 {
		return out, failure(400, "invalid_input", "A request field exceeds its size limit.")
	}
	if out.Instructions != "" {
		out.InstructionMode = "task_text"
	}
	var messages []uhpInputMessage
	if json.Unmarshal(out.Input, &messages) == nil {
		for _, message := range messages {
			if message.Role == "system" || message.Role == "developer" {
				out.InstructionMode = "task_text"
			}
		}
	}
	sort.Strings(out.IgnoredFields)
	return out, nil
}

type uhpInputPart struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Filename string `json:"filename"`
	FileID   string `json:"file_id"`
	FileData string `json:"file_data"`
	ImageURL string `json:"image_url"`
}
type uhpInputMessage struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func uhpContentParts(raw json.RawMessage) ([]uhpInputPart, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return []uhpInputPart{{Type: "input_text", Text: text}}, nil
	}
	var parts []uhpInputPart
	if json.Unmarshal(raw, &parts) != nil || parts == nil {
		return nil, failure(400, "invalid_input", "Message content must be text or an array of content parts.")
	}
	return parts, nil
}

func (s *Server) uhpPrepareInput(req uhpRequest, owner, wid string) (Input, error) {
	out := Input{Files: []string{}}
	var text string
	var selected []uhpInputPart
	guidance := []string{}
	if req.Instructions != "" {
		guidance = append(guidance, req.Instructions)
	}
	if json.Unmarshal(req.Input, &text) == nil && !bytes.Equal(bytes.TrimSpace(req.Input), []byte("null")) {
		selected = []uhpInputPart{{Type: "input_text", Text: text}}
	} else {
		var messages []uhpInputMessage
		if json.Unmarshal(req.Input, &messages) != nil || len(messages) == 0 {
			return out, failure(400, "invalid_input", "input must be text or a non-empty array of messages.")
		}
		for _, msg := range messages {
			if msg.Type != "" && msg.Type != "message" {
				return out, failure(400, "invalid_input", "Only message input items are supported.")
			}
			parts, err := uhpContentParts(msg.Content)
			if err != nil {
				return out, err
			}
			switch msg.Role {
			case "", "user":
				// input_history is false: only the last user message is a new
				// task; earlier assistant messages are not imported turns.
				selected = parts
			case "system", "developer":
				for _, part := range parts {
					if part.Type != "input_text" && part.Type != "output_text" {
						return out, failure(400, "invalid_input", "Task guidance accepts text only.")
					}
					guidance = append(guidance, part.Text)
				}
			case "assistant":
				for _, part := range parts {
					if part.Type != "input_text" && part.Type != "output_text" {
						return out, failure(400, "invalid_input", "Assistant history accepts text only.")
					}
				}
			default:
				return out, failure(400, "invalid_input", "The message role is not supported.")
			}
		}
	}
	if len(selected) == 0 {
		return out, failure(400, "invalid_input", "The input must include a user message.")
	}
	texts := []string{}
	for _, part := range selected {
		switch part.Type {
		case "input_text":
			texts = append(texts, part.Text)
		case "input_file", "input_image":
			inline := part.FileData
			if part.ImageURL != "" {
				if inline != "" {
					return out, failure(400, "invalid_input", "An input file must have one source.")
				}
				inline = part.ImageURL
			}
			if (part.FileID == "") == (inline == "") {
				return out, failure(400, "invalid_input", "An input file needs exactly one of file_id or inline data.")
			}
			var path string
			var err error
			files := uhpFiles{Manager: s.Manager}
			if part.FileID != "" {
				if part.Type == "input_image" {
					path, err = files.PrepareImageInput(owner, wid, part.FileID)
				} else {
					path, err = files.PrepareInput(owner, wid, part.FileID)
				}
			} else {
				if !strings.HasPrefix(inline, "data:") {
					return out, failure(422, "rundesk_remote_image_unsupported", "Remote file URLs are not supported; use a data URL or upload.")
				}
				name := part.Filename
				if name == "" {
					name = "input.bin"
				}
				if part.Type == "input_image" {
					path, err = files.InlineImage(owner, wid, name, inline)
				} else {
					path, err = files.Inline(owner, wid, name, inline)
				}
			}
			if err != nil {
				return out, err
			}
			out.Files = append(out.Files, path)
		default:
			return out, failure(400, "invalid_input", "The input content type is not supported.")
		}
	}
	out.Text = strings.Join(texts, "\n")
	if strings.TrimSpace(out.Text) == "" && len(out.Files) > 0 {
		out.Text = "Process the attached files."
	}
	if len(guidance) > 0 {
		// RunDesk's native input contract has no per-turn system-role field.
		// Preserve guidance explicitly as task text, without changing global
		// harness instructions or claiming a different instruction priority.
		out.Text = "Additional task guidance:\n" + strings.Join(guidance, "\n") + "\n\nTask:\n" + out.Text
	}
	if strings.TrimSpace(out.Text) == "" || len(out.Text) > 256<<10 || len(out.Files) > 64 {
		return out, failure(400, "invalid_input", "Task input exceeds the text or attachment limit, or is empty.")
	}
	return out, nil
}
