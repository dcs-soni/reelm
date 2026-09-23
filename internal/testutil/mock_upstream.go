package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"time"
)

// MockUpstream simulates upstream LLM API providers (OpenAI, Anthropic, Gemini).
type MockUpstream struct {
	server    *httptest.Server
	callCount int64
}

// NewMockOpenAI creates a mock OpenAI server with streaming support.
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

		isStream, _ := reqBody["stream"].(bool)

		if isStream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(http.StatusOK)

			flusher, ok := w.(http.Flusher)
			chunks := []string{
				`data: {"id":"chatcmpl-stream-1","choices":[{"delta":{"role":"assistant","content":"Streamed"}}]}`,
				`data: {"id":"chatcmpl-stream-2","choices":[{"delta":{"content":" response"}}]}`,
				`data: {"id":"chatcmpl-stream-3","choices":[{"delta":{"content":" complete."}}]}`,
				`data: [DONE]`,
			}

			for _, c := range chunks {
				_, _ = fmt.Fprintf(w, "%s\n\n", c)
				if ok {
					flusher.Flush()
				}
				time.Sleep(2 * time.Millisecond)
			}
			return
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

	// Anthropic Messages endpoint
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&m.callCount, 1)

		var reqBody map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&reqBody)

		model, _ := reqBody["model"].(string)
		if model == "" {
			model = "claude-3-5-sonnet"
		}

		resp := map[string]interface{}{
			"id":    fmt.Sprintf("msg_%d", time.Now().UnixNano()),
			"type":  "message",
			"role":  "assistant",
			"model": model,
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": "Hello from mock Anthropic Claude!",
				},
			},
			"stop_reason": "end_turn",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	// Gemini endpoint
	mux.HandleFunc("/v1beta/models/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&m.callCount, 1)

		isStream := strings.Contains(r.URL.Path, ":streamGenerateContent")
		if isStream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, ok := w.(http.Flusher)
			chunks := []string{
				`data: {"candidates":[{"content":{"parts":[{"text":"Gemini"}]}}]}`,
				`data: {"candidates":[{"content":{"parts":[{"text":" stream"}]}}]}`,
			}
			for _, c := range chunks {
				_, _ = fmt.Fprintf(w, "%s\n\n", c)
				if ok {
					flusher.Flush()
				}
				time.Sleep(2 * time.Millisecond)
			}
			return
		}

		resp := map[string]interface{}{
			"candidates": []map[string]interface{}{
				{
					"content": map[string]interface{}{
						"role": "model",
						"parts": []map[string]interface{}{
							{"text": "Hello from mock Google Gemini!"},
						},
					},
					"finishReason": "STOP",
				},
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
