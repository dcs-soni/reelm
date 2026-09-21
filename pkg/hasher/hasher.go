package hasher

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Hasher interface for computing deterministic hashes of canonicalized requests.
type Hasher interface {
	Hash(canonic *CanonicRequest) (string, error)
}

// CanonicRequest is a provider-agnostic, normalized representation of an LLM API call.
type CanonicRequest struct {
	Namespace   string                 `json:"namespace,omitempty"`
	Provider    string                 `json:"provider"`
	Endpoint    string                 `json:"endpoint"`
	Method      string                 `json:"method"`
	Model       string                 `json:"model"`
	Messages    []Message              `json:"messages"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
	Tools       []Tool                 `json:"tools,omitempty"`
	ExtraFields map[string]interface{} `json:"extra_fields,omitempty"`
}

// Message represents a standardized chat message across providers.
type Message struct {
	Role       string      `json:"role"`
	Content    string      `json:"content"`
	Name       string      `json:"name,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
}

// ToolCall represents a tool or function invocation request by the model.
type ToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // canonicalized JSON string
	} `json:"function"`
}

// Tool represents a tool/function definition passed to the model.
type Tool struct {
	Type     string                 `json:"type"`
	Function map[string]interface{} `json:"function"`
}

// DefaultHasher implements standard SHA-256 canonical hashing.
type DefaultHasher struct{}

// NewDefaultHasher returns a new instance of DefaultHasher.
func NewDefaultHasher() *DefaultHasher {
	return &DefaultHasher{}
}

// Hash computes the SHA-256 hash of the canonicalized request.
func (h *DefaultHasher) Hash(canonic *CanonicRequest) (string, error) {
	if canonic == nil {
		return "", fmt.Errorf("canonic request cannot be nil")
	}

	bytes, err := json.Marshal(canonic)
	if err != nil {
		return "", fmt.Errorf("failed to marshal canonic request: %w", err)
	}

	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:]), nil
}
