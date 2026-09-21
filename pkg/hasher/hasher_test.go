package hasher_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dcs-soni/reelm/pkg/hasher"
	"github.com/dcs-soni/reelm/pkg/hasher/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasherDeterminism(t *testing.T) {
	h := hasher.NewDefaultHasher()
	canon := providers.NewOpenAICanonicalizer()

	// JSON 1: standard ordering
	body1 := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "What is the capital of France?"}
		],
		"temperature": 0.7,
		"user": "user-123",
		"stream": false
	}`)

	// JSON 2: reordered keys, different user ID, stream: true, extra whitespace
	body2 := []byte(`{
		"temperature": 0.7,
		"stream": true,
		"user": "different-user-456",
		"messages": [
			{"role": "system", "content": "You are a helpful assistant.  "},
			{"content": "What is the capital of France?", "role": "user"}
		],
		"model": "gpt-4o"
	}`)

	req1 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	canonic1, err := canon.Canonicalize(req1, body1)
	require.NoError(t, err)

	canonic2, err := canon.Canonicalize(req2, body2)
	require.NoError(t, err)

	hash1, err := h.Hash(canonic1)
	require.NoError(t, err)

	hash2, err := h.Hash(canonic2)
	require.NoError(t, err)

	assert.Equal(t, hash1, hash2, "Identical semantic requests must produce identical hashes regardless of non-deterministic fields")
}

func TestHasherDifferentiatesChanges(t *testing.T) {
	h := hasher.NewDefaultHasher()
	canon := providers.NewOpenAICanonicalizer()

	baseBody := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "Hello"}]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	canonicBase, err := canon.Canonicalize(req, baseBody)
	require.NoError(t, err)
	hashBase, err := h.Hash(canonicBase)
	require.NoError(t, err)

	// 1. Different model
	diffModelBody := []byte(`{
		"model": "gpt-4-turbo",
		"messages": [{"role": "user", "content": "Hello"}]
	}`)
	canonicModel, _ := canon.Canonicalize(req, diffModelBody)
	hashModel, _ := h.Hash(canonicModel)
	assert.NotEqual(t, hashBase, hashModel, "Different model must produce different hash")

	// 2. Different prompt content
	diffPromptBody := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "Hello world"}]
	}`)
	canonicPrompt, _ := canon.Canonicalize(req, diffPromptBody)
	hashPrompt, _ := h.Hash(canonicPrompt)
	assert.NotEqual(t, hashBase, hashPrompt, "Different prompt must produce different hash")

	// 3. Different namespace
	reqNamespace := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	reqNamespace.Header.Set("X-Reelm-Namespace", "suite-a")
	canonicNS, _ := canon.Canonicalize(reqNamespace, baseBody)
	hashNS, _ := h.Hash(canonicNS)
	assert.NotEqual(t, hashBase, hashNS, "Different namespace must isolate hashes")
}

func TestToolCanonicalization(t *testing.T) {
	h := hasher.NewDefaultHasher()
	canon := providers.NewOpenAICanonicalizer()

	// Tools in order A, B
	body1 := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "Get weather"}],
		"tools": [
			{"type": "function", "function": {"name": "get_stock_price"}},
			{"type": "function", "function": {"name": "get_weather"}}
		]
	}`)

	// Tools in order B, A
	body2 := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "Get weather"}],
		"tools": [
			{"type": "function", "function": {"name": "get_weather"}},
			{"type": "function", "function": {"name": "get_stock_price"}}
		]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c1, err := canon.Canonicalize(req, body1)
	require.NoError(t, err)
	c2, err := canon.Canonicalize(req, body2)
	require.NoError(t, err)

	hash1, _ := h.Hash(c1)
	hash2, _ := h.Hash(c2)

	assert.Equal(t, hash1, hash2, "Reordered tool functions must produce identical hash")
}
