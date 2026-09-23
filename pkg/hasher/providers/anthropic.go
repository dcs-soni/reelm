package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dcs-soni/reelm/pkg/hasher"
)

// AnthropicCanonicalizer parses and canonicalizes Anthropic Messages API requests (/v1/messages).
type AnthropicCanonicalizer struct{}

// NewAnthropicCanonicalizer returns a new AnthropicCanonicalizer instance.
func NewAnthropicCanonicalizer() *AnthropicCanonicalizer {
	return &AnthropicCanonicalizer{}
}

type anthropicRequestSchema struct {
	Model         string                   `json:"model"`
	Messages      []anthropicMessageSchema `json:"messages"`
	System        interface{}              `json:"system,omitempty"` // string or array of text blocks
	MaxTokens     *int                     `json:"max_tokens,omitempty"`
	Temperature   *float64                 `json:"temperature,omitempty"`
	TopP          *float64                 `json:"top_p,omitempty"`
	TopK          *int                     `json:"top_k,omitempty"`
	StopSequences []string                 `json:"stop_sequences,omitempty"`
	Tools         []anthropicToolSchema    `json:"tools,omitempty"`
	ToolChoice    interface{}              `json:"tool_choice,omitempty"`
}

type anthropicMessageSchema struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []anthropicContentBlock
}

type anthropicContentBlock struct {
	Type  string                 `json:"type"`
	Text  string                 `json:"text,omitempty"`
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
}

type anthropicToolSchema struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// Canonicalize transforms an HTTP request to Anthropic into a canonicalized CanonicRequest.
func (c *AnthropicCanonicalizer) Canonicalize(req *http.Request, body []byte) (*hasher.CanonicRequest, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("empty request body")
	}

	var parsed anthropicRequestSchema
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse Anthropic request body: %w", err)
	}

	messages := make([]hasher.Message, 0, len(parsed.Messages))

	// System message handling if present
	if parsed.System != nil {
		sysText := ""
		switch v := parsed.System.(type) {
		case string:
			sysText = hasher.NormalizeText(v)
		case []interface{}:
			b, _ := json.Marshal(v)
			canonBytes, _ := hasher.CanonicalizeJSON(b)
			sysText = string(canonBytes)
		}
		if sysText != "" {
			messages = append(messages, hasher.Message{
				Role:    "system",
				Content: sysText,
			})
		}
	}

	// Canonicalize messages
	for _, m := range parsed.Messages {
		contentStr := ""
		var toolCalls []hasher.ToolCall

		switch v := m.Content.(type) {
		case string:
			contentStr = hasher.NormalizeText(v)
		case []interface{}:
			var textParts []string
			for _, part := range v {
				partMap, ok := part.(map[string]interface{})
				if !ok {
					continue
				}
				pType, _ := partMap["type"].(string)
				switch pType {
				case "text":
					if txt, ok := partMap["text"].(string); ok {
						textParts = append(textParts, hasher.NormalizeText(txt))
					}
				case "tool_use":
					tcID, _ := partMap["id"].(string)
					tcName, _ := partMap["name"].(string)
					argsBytes, _ := json.Marshal(partMap["input"])
					canonArgs, _ := hasher.CanonicalizeJSON(argsBytes)
					toolCalls = append(toolCalls, hasher.ToolCall{
						ID:   tcID,
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name:      tcName,
							Arguments: string(canonArgs),
						},
					})
				case "tool_result":
					resBytes, _ := json.Marshal(partMap)
					canonRes, _ := hasher.CanonicalizeJSON(resBytes)
					textParts = append(textParts, string(canonRes))
				default:
					b, _ := json.Marshal(partMap)
					textParts = append(textParts, string(b))
				}
			}
			contentStr = strings.Join(textParts, "\n")
		}

		messages = append(messages, hasher.Message{
			Role:      strings.ToLower(strings.TrimSpace(m.Role)),
			Content:   contentStr,
			ToolCalls: toolCalls,
		})
	}

	// Parameters
	params := make(map[string]interface{})
	if parsed.MaxTokens != nil {
		params["max_tokens"] = *parsed.MaxTokens
	}
	if parsed.Temperature != nil {
		params["temperature"] = *parsed.Temperature
	}
	if parsed.TopP != nil {
		params["top_p"] = *parsed.TopP
	}
	if parsed.TopK != nil {
		params["top_k"] = *parsed.TopK
	}
	if len(parsed.StopSequences) > 0 {
		params["stop_sequences"] = parsed.StopSequences
	}
	if parsed.ToolChoice != nil {
		params["tool_choice"] = parsed.ToolChoice
	}

	// Canonicalize tools
	var tools []hasher.Tool
	if len(parsed.Tools) > 0 {
		tools = make([]hasher.Tool, len(parsed.Tools))
		for i, t := range parsed.Tools {
			fnMap := map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.InputSchema,
			}
			tools[i] = hasher.Tool{
				Type:     "function",
				Function: fnMap,
			}
		}
		tools = hasher.CanonicalizeTools(tools)
	}

	namespace := ""
	endpoint := "/v1/messages"
	method := "POST"
	if req != nil {
		namespace = req.Header.Get("X-Reelm-Namespace")
		if req.URL != nil && req.URL.Path != "" {
			endpoint = req.URL.Path
		}
		if req.Method != "" {
			method = req.Method
		}
	}

	return &hasher.CanonicRequest{
		Namespace:  namespace,
		Provider:   "anthropic",
		Endpoint:   endpoint,
		Method:     method,
		Model:      parsed.Model,
		Messages:   messages,
		Parameters: params,
		Tools:      tools,
	}, nil
}
