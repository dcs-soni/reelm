package providers

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/dcs-soni/reelm/pkg/hasher"
)

// Canonicalizer is an interface for provider-specific request canonicalizers.
type Canonicalizer interface {
	Canonicalize(req *http.Request, body []byte) (*hasher.CanonicRequest, error)
}

// Registry manages registered provider canonicalizers.
type Registry struct {
	mu             sync.RWMutex
	canonicalizers map[string]Canonicalizer
}

var (
	defaultRegistry = NewRegistry()
)

// NewRegistry creates a new Registry with standard built-in providers.
func NewRegistry() *Registry {
	r := &Registry{
		canonicalizers: make(map[string]Canonicalizer),
	}
	r.Register("openai", NewOpenAICanonicalizer())
	r.Register("anthropic", NewAnthropicCanonicalizer())
	r.Register("gemini", NewGeminiCanonicalizer())
	r.Register("azure", NewAzureCanonicalizer())
	return r
}

// DefaultRegistry returns the singleton global provider registry.
func DefaultRegistry() *Registry {
	return defaultRegistry
}

// Register adds or replaces a canonicalizer for a given provider name.
func (r *Registry) Register(provider string, c Canonicalizer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.canonicalizers[strings.ToLower(provider)] = c
}

// Get retrieves the canonicalizer for the specified provider.
func (r *Registry) Get(provider string) (Canonicalizer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.canonicalizers[strings.ToLower(provider)]
	return c, ok
}

// DetectProvider inspects the request URL, headers, and host to identify the upstream provider.
func (r *Registry) DetectProvider(req *http.Request) (string, Canonicalizer, error) {
	if req == nil {
		return "", nil, fmt.Errorf("request cannot be nil")
	}

	path := ""
	host := ""
	if req.URL != nil {
		path = req.URL.Path
	}
	if req.Host != "" {
		host = req.Host
	}

	// 1. Explicit header override (X-Reelm-Provider)
	if p := req.Header.Get("X-Reelm-Provider"); p != "" {
		if c, ok := r.Get(p); ok {
			return strings.ToLower(p), c, nil
		}
	}

	// 2. Azure OpenAI detection: /openai/deployments/ or *.openai.azure.com
	if strings.Contains(path, "/openai/deployments") || strings.Contains(host, ".openai.azure.com") {
		if c, ok := r.Get("azure"); ok {
			return "azure", c, nil
		}
	}

	// 3. Anthropic detection: /v1/messages, api.anthropic.com, or x-api-key / anthropic-version
	if strings.Contains(path, "/v1/messages") ||
		strings.Contains(path, "/v1/complete") ||
		strings.Contains(host, "anthropic.com") ||
		req.Header.Get("anthropic-version") != "" ||
		req.Header.Get("x-api-key") != "" {
		if c, ok := r.Get("anthropic"); ok {
			return "anthropic", c, nil
		}
	}

	// 4. Gemini detection: :generateContent, :streamGenerateContent, generativelanguage.googleapis.com
	if strings.Contains(path, ":generateContent") ||
		strings.Contains(path, ":streamGenerateContent") ||
		strings.Contains(path, "/v1beta/models") ||
		strings.Contains(host, "generativelanguage.googleapis.com") {
		if c, ok := r.Get("gemini"); ok {
			return "gemini", c, nil
		}
	}

	// 5. OpenAI detection heuristics: /v1/chat/completions, /v1/models, api.openai.com
	if strings.Contains(path, "/chat/completions") ||
		strings.Contains(path, "/v1/models") ||
		strings.Contains(host, "api.openai.com") ||
		strings.HasPrefix(path, "/v1/") {
		if c, ok := r.Get("openai"); ok {
			return "openai", c, nil
		}
	}

	// 6. Default fallback to OpenAI
	if c, ok := r.Get("openai"); ok {
		return "openai", c, nil
	}

	return "", nil, fmt.Errorf("could not detect provider for request path %q", path)
}
