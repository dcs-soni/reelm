package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dcs-soni/reelm/pkg/hasher"
)

// OpenAICanonicalizer parses and canonicalizes OpenAI Chat Completions requests.
type OpenAICanonicalizer struct{}

// NewOpenAICanonicalizer returns a new OpenAICanonicalizer instance.
func NewOpenAICanonicalizer() *OpenAICanonicalizer {
	return &OpenAICanonicalizer{}
}

// openAIRequestSchema is the internal unmarshal target for OpenAI Chat Completion requests.
type openAIRequestSchema struct {
	Model               string                   `json:"model"`
	Messages            []openAIMessageSchema    `json:"messages"`
	Temperature         *float64                 `json:"temperature,omitempty"`
	TopP                *float64                 `json:"top_p,omitempty"`
	MaxTokens           *int                     `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int                     `json:"max_completion_tokens,omitempty"`
	PresencePenalty     *float64                 `json:"presence_penalty,omitempty"`
	FrequencyPenalty    *float64                 `json:"frequency_penalty,omitempty"`
	ResponseFormat      interface{}              `json:"response_format,omitempty"`
	Tools               []hasher.Tool            `json:"tools,omitempty"`
	ToolChoice          interface{}              `json:"tool_choice,omitempty"`
	Stop                interface{}              `json:"stop,omitempty"`
	Extra               map[string]interface{}   `json:"-"`
}

type openAIMessageSchema struct {
	Role       string            `json:"role"`
	Content    interface{}       `json:"content"` // can be string or array of content parts
	Name       string            `json:"name,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall  `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Canonicalize transforms an HTTP request to OpenAI into a canonicalized CanonicRequest.
func (c *OpenAICanonicalizer) Canonicalize(req *http.Request, body []byte) (*hasher.CanonicRequest, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("empty request body")
	}

	var parsed openAIRequestSchema
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse OpenAI request body: %w", err)
	}

	// Canonicalize messages
	messages := make([]hasher.Message, 0, len(parsed.Messages))
	for _, m := range parsed.Messages {
		contentStr := ""
		switch v := m.Content.(type) {
		case string:
			contentStr = hasher.NormalizeText(v)
		case []interface{}:
			// Handle multimodal array of parts (e.g., text, image_url)
			partsBytes, _ := json.Marshal(v)
			canonBytes, _ := hasher.CanonicalizeJSON(partsBytes)
			contentStr = string(canonBytes)
		default:
			if v != nil {
				b, _ := json.Marshal(v)
				contentStr = string(b)
			}
		}

		msg := hasher.Message{
			Role:       strings.ToLower(strings.TrimSpace(m.Role)),
			Content:    contentStr,
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
		}

		if len(m.ToolCalls) > 0 {
			msg.ToolCalls = make([]hasher.ToolCall, len(m.ToolCalls))
			for i, tc := range m.ToolCalls {
				// Canonicalize arguments JSON if valid
				canonArgs, err := hasher.CanonicalizeJSON([]byte(tc.Function.Arguments))
				argsStr := tc.Function.Arguments
				if err == nil {
					argsStr = string(canonArgs)
				}
				msg.ToolCalls[i] = hasher.ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      tc.Function.Name,
						Arguments: argsStr,
					},
				}
			}
		}

		messages = append(messages, msg)
	}

	// Parameters
	params := make(map[string]interface{})
	if parsed.Temperature != nil {
		params["temperature"] = *parsed.Temperature
	}
	if parsed.TopP != nil {
		params["top_p"] = *parsed.TopP
	}
	if parsed.MaxTokens != nil {
		params["max_tokens"] = *parsed.MaxTokens
	}
	if parsed.MaxCompletionTokens != nil {
		params["max_completion_tokens"] = *parsed.MaxCompletionTokens
	}
	if parsed.PresencePenalty != nil {
		params["presence_penalty"] = *parsed.PresencePenalty
	}
	if parsed.FrequencyPenalty != nil {
		params["frequency_penalty"] = *parsed.FrequencyPenalty
	}
	if parsed.ResponseFormat != nil {
		params["response_format"] = parsed.ResponseFormat
	}
	if parsed.ToolChoice != nil {
		params["tool_choice"] = parsed.ToolChoice
	}
	if parsed.Stop != nil {
		params["stop"] = parsed.Stop
	}

	// Tools
	tools := hasher.CanonicalizeTools(parsed.Tools)

	// Namespace from header
	namespace := ""
	if req != nil {
		namespace = req.Header.Get("X-Reelm-Namespace")
	}

	endpoint := "/v1/chat/completions"
	method := "POST"
	if req != nil {
		if req.URL != nil && req.URL.Path != "" {
			endpoint = req.URL.Path
		}
		if req.Method != "" {
			method = req.Method
		}
	}

	return &hasher.CanonicRequest{
		Namespace:  namespace,
		Provider:   "openai",
		Endpoint:   endpoint,
		Method:     method,
		Model:      parsed.Model,
		Messages:   messages,
		Parameters: params,
		Tools:      tools,
	}, nil
}
