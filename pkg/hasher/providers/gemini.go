package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dcs-soni/reelm/pkg/hasher"
)

// GeminiCanonicalizer parses and canonicalizes Google Gemini API requests.
type GeminiCanonicalizer struct{}

// NewGeminiCanonicalizer returns a new GeminiCanonicalizer instance.
func NewGeminiCanonicalizer() *GeminiCanonicalizer {
	return &GeminiCanonicalizer{}
}

type geminiRequestSchema struct {
	Contents          []geminiContentSchema  `json:"contents"`
	SystemInstruction *geminiContentSchema   `json:"systemInstruction,omitempty"`
	GenerationConfig  map[string]interface{} `json:"generationConfig,omitempty"`
	SafetySettings    interface{}            `json:"safetySettings,omitempty"`
	Tools             []interface{}          `json:"tools,omitempty"`
	ToolConfig        interface{}            `json:"toolConfig,omitempty"`
}

type geminiContentSchema struct {
	Role  string            `json:"role,omitempty"`
	Parts []geminiPartSchema `json:"parts"`
}

type geminiPartSchema struct {
	Text             string      `json:"text,omitempty"`
	InlineData       interface{} `json:"inlineData,omitempty"`
	FunctionCall     interface{} `json:"functionCall,omitempty"`
	FunctionResponse interface{} `json:"functionResponse,omitempty"`
}

// Canonicalize transforms an HTTP request to Gemini into a canonicalized CanonicRequest.
func (c *GeminiCanonicalizer) Canonicalize(req *http.Request, body []byte) (*hasher.CanonicRequest, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("empty request body")
	}

	var parsed geminiRequestSchema
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse Gemini request body: %w", err)
	}

	// Extract model from URL path, e.g. /v1beta/models/gemini-1.5-pro:generateContent
	model := "gemini-pro"
	endpoint := "/v1beta/models/gemini-pro:generateContent"
	method := "POST"
	namespace := ""

	if req != nil {
		namespace = req.Header.Get("X-Reelm-Namespace")
		if req.URL != nil && req.URL.Path != "" {
			endpoint = req.URL.Path
			// parse model between "models/" and ":"
			if idx := strings.Index(endpoint, "models/"); idx != -1 {
				rest := endpoint[idx+len("models/"):]
				if colonIdx := strings.Index(rest, ":"); colonIdx != -1 {
					model = rest[:colonIdx]
				} else {
					model = rest
				}
			}
		}
		if req.Method != "" {
			method = req.Method
		}
	}

	var messages []hasher.Message

	// System instruction if present
	if parsed.SystemInstruction != nil {
		var sysParts []string
		for _, p := range parsed.SystemInstruction.Parts {
			if p.Text != "" {
				sysParts = append(sysParts, hasher.NormalizeText(p.Text))
			}
		}
		if len(sysParts) > 0 {
			messages = append(messages, hasher.Message{
				Role:    "system",
				Content: strings.Join(sysParts, "\n"),
			})
		}
	}

	// Canonicalize conversation contents
	for _, content := range parsed.Contents {
		role := content.Role
		if role == "" || role == "user" {
			role = "user"
		} else if role == "model" {
			role = "assistant"
		}

		var textParts []string
		var toolCalls []hasher.ToolCall

		for _, p := range content.Parts {
			if p.Text != "" {
				textParts = append(textParts, hasher.NormalizeText(p.Text))
			}
			if p.FunctionCall != nil {
				fcBytes, _ := json.Marshal(p.FunctionCall)
				canonBytes, _ := hasher.CanonicalizeJSON(fcBytes)
				var fcMap map[string]interface{}
				_ = json.Unmarshal(canonBytes, &fcMap)
				fnName, _ := fcMap["name"].(string)
				argsBytes, _ := json.Marshal(fcMap["args"])
				canonArgs, _ := hasher.CanonicalizeJSON(argsBytes)

				toolCalls = append(toolCalls, hasher.ToolCall{
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      fnName,
						Arguments: string(canonArgs),
					},
				})
			}
			if p.FunctionResponse != nil {
				frBytes, _ := json.Marshal(p.FunctionResponse)
				canonBytes, _ := hasher.CanonicalizeJSON(frBytes)
				textParts = append(textParts, string(canonBytes))
			}
			if p.InlineData != nil {
				idBytes, _ := json.Marshal(p.InlineData)
				canonBytes, _ := hasher.CanonicalizeJSON(idBytes)
				textParts = append(textParts, string(canonBytes))
			}
		}

		messages = append(messages, hasher.Message{
			Role:      role,
			Content:   strings.Join(textParts, "\n"),
			ToolCalls: toolCalls,
		})
	}

	// Parameters
	params := make(map[string]interface{})
	if parsed.GenerationConfig != nil {
		for k, v := range parsed.GenerationConfig {
			params[k] = v
		}
	}
	if parsed.SafetySettings != nil {
		params["safetySettings"] = parsed.SafetySettings
	}

	// Canonicalize tools if present
	var tools []hasher.Tool
	if len(parsed.Tools) > 0 {
		for _, t := range parsed.Tools {
			if tMap, ok := t.(map[string]interface{}); ok {
				if fDecls, ok := tMap["functionDeclarations"].([]interface{}); ok {
					for _, fd := range fDecls {
						if fdMap, ok := fd.(map[string]interface{}); ok {
							fnName, _ := fdMap["name"].(string)
							tools = append(tools, hasher.Tool{
								Type: "function",
								Function: map[string]interface{}{
									"name":        fnName,
									"description": fdMap["description"],
									"parameters":  fdMap["parameters"],
								},
							})
						}
					}
				}
			}
		}
		tools = hasher.CanonicalizeTools(tools)
	}

	return &hasher.CanonicRequest{
		Namespace:  namespace,
		Provider:   "gemini",
		Endpoint:   endpoint,
		Method:     method,
		Model:      model,
		Messages:   messages,
		Parameters: params,
		Tools:      tools,
	}, nil
}
