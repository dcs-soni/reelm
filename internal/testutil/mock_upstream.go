package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"
)

// MockUpstream simulates an upstream LLM API provider like OpenAI.
type MockUpstream struct {
	server    *httptest.Server
	callCount int64
}

// NewMockOpenAI creates a mock OpenAI server.
func NewMockOpenAI() *MockUpstream {
	m := &MockUpstream{}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&m.callCount, 1)

		var reqBody map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&reqBody)

		model, _ := reqBody["model"].(string)
		if model == "" {
			model = "gpt-4o"
		}

		resp := map[string]interface{}{
			"id":      fmt.Sprintf("chatcmpl-mock-%d", time.Now().UnixNano()),
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "This is a deterministic mock completion from mock OpenAI.",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     12,
				"completion_tokens": 8,
				"total_tokens":      20,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	m.server = httptest.NewServer(mux)
	return m
}

// URL returns the base URL of the mock upstream server.
func (m *MockUpstream) URL() string {
	return m.server.URL
}

// CallCount returns the total number of calls received by the mock server.
func (m *MockUpstream) CallCount() int64 {
	return atomic.LoadInt64(&m.callCount)
}

// Close shuts down the test server.
func (m *MockUpstream) Close() {
	m.server.Close()
}
