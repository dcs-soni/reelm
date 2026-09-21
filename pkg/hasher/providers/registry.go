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
	mu            sync.RWMutex
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

	path := req.URL.Path

	// 1. Explicit header override
	if p := req.Header.Get("X-Reelm-Provider"); p != "" {
		if c, ok := r.Get(p); ok {
			return strings.ToLower(p), c, nil
		}
	}

	// 2. OpenAI detection heuristics
	if strings.Contains(path, "/chat/completions") ||
		strings.Contains(path, "/v1/models") ||
		strings.HasPrefix(path, "/v1/") {
		if c, ok := r.Get("openai"); ok {
			return "openai", c, nil
		}
	}

	// 3. Fallback to OpenAI default if only OpenAI registered
	if c, ok := r.Get("openai"); ok {
		return "openai", c, nil
	}

	return "", nil, fmt.Errorf("could not detect provider for request path %q", path)
}
