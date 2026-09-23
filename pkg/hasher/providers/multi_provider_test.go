package providers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dcs-soni/reelm/pkg/hasher"
	"github.com/dcs-soni/reelm/pkg/hasher/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnthropicCanonicalizer(t *testing.T) {
	c := providers.NewAnthropicCanonicalizer()
	h := hasher.NewDefaultHasher()

	body1 := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"system": "You are an expert Go engineer.",
		"temperature": 0.5,
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "Can you check the weather?"}
				]
			}
		],
		"tools": [
			{
				"name": "get_stock_price",
				"description": "Stock price check",
				"input_schema": {"type": "object", "properties": {"symbol": {"type": "string"}}}
			},
			{
				"name": "get_weather",
				"description": "Weather check",
				"input_schema": {"type": "object", "properties": {"city": {"type": "string"}}}
			}
		]
	}`)

	// Reordered tools and simple string content instead of array
	body2 := []byte(`{
		"temperature": 0.5,
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"system": "You are an expert Go engineer.",
		"tools": [
			{
				"name": "get_weather",
				"description": "Weather check",
				"input_schema": {"type": "object", "properties": {"city": {"type": "string"}}}
			},
			{
				"name": "get_stock_price",
				"description": "Stock price check",
				"input_schema": {"type": "object", "properties": {"symbol": {"type": "string"}}}
			}
		],
		"messages": [
			{"role": "user", "content": "Can you check the weather?"}
		]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c1, err := c.Canonicalize(req, body1)
	require.NoError(t, err)
	assert.Equal(t, "anthropic", c1.Provider)
	assert.Equal(t, "claude-3-5-sonnet-20241022", c1.Model)
	assert.Len(t, c1.Messages, 2) // system + user
	assert.Len(t, c1.Tools, 2)

	c2, err := c.Canonicalize(req, body2)
	require.NoError(t, err)

	hash1, err := h.Hash(c1)
	require.NoError(t, err)
	hash2, err := h.Hash(c2)
	require.NoError(t, err)

	assert.Equal(t, hash1, hash2, "Anthropic requests with reordered keys and tools must produce identical hashes")
}

func TestGeminiCanonicalizer(t *testing.T) {
	c := providers.NewGeminiCanonicalizer()
	h := hasher.NewDefaultHasher()

	body := []byte(`{
		"contents": [
			{
				"role": "user",
				"parts": [{"text": "Explain quantum computing in one sentence."}]
			}
		],
		"systemInstruction": {
			"parts": [{"text": "Be concise."}]
		},
		"generationConfig": {
			"temperature": 0.2,
			"topP": 0.8
		}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-1.5-pro:generateContent", nil)
	req.Header.Set("X-Reelm-Namespace", "gemini-suite")

	canonic, err := c.Canonicalize(req, body)
	require.NoError(t, err)

	assert.Equal(t, "gemini", canonic.Provider)
	assert.Equal(t, "gemini-1.5-pro", canonic.Model)
	assert.Equal(t, "gemini-suite", canonic.Namespace)
	assert.Len(t, canonic.Messages, 2) // system + user
	assert.Equal(t, "Be concise.", canonic.Messages[0].Content)

	hash, err := h.Hash(canonic)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
}

func TestAzureCanonicalizer(t *testing.T) {
	c := providers.NewAzureCanonicalizer()
	h := hasher.NewDefaultHasher()

	body := []byte(`{
		"messages": [{"role": "user", "content": "Hello Azure OpenAI"}]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/openai/deployments/my-gpt4-deployment/chat/completions?api-version=2024-02-15-preview", nil)
	canonic, err := c.Canonicalize(req, body)
	require.NoError(t, err)

	assert.Equal(t, "azure", canonic.Provider)
	assert.Equal(t, "my-gpt4-deployment", canonic.Model)
	assert.Len(t, canonic.Messages, 1)

	hash, err := h.Hash(canonic)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
}

func TestRegistryMultiProviderDetection(t *testing.T) {
	reg := providers.DefaultRegistry()

	// 1. Anthropic by path
	reqAnthropic := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	p, c, err := reg.DetectProvider(reqAnthropic)
	require.NoError(t, err)
	assert.Equal(t, "anthropic", p)
	assert.NotNil(t, c)

	// 2. Gemini by path
	reqGemini := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-1.5-flash:streamGenerateContent", nil)
	p, c, err = reg.DetectProvider(reqGemini)
	require.NoError(t, err)
	assert.Equal(t, "gemini", p)
	assert.NotNil(t, c)

	// 3. Azure by path
	reqAzure := httptest.NewRequest(http.MethodPost, "/openai/deployments/prod-gpt-4o/chat/completions", nil)
	p, c, err = reg.DetectProvider(reqAzure)
	require.NoError(t, err)
	assert.Equal(t, "azure", p)
	assert.NotNil(t, c)

	// 4. OpenAI by path
	reqOpenAI := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	p, c, err = reg.DetectProvider(reqOpenAI)
	require.NoError(t, err)
	assert.Equal(t, "openai", p)
	assert.NotNil(t, c)

	// 5. Header override
	reqOverride := httptest.NewRequest(http.MethodPost, "/any/endpoint", nil)
	reqOverride.Header.Set("X-Reelm-Provider", "anthropic")
	p, c, err = reg.DetectProvider(reqOverride)
	require.NoError(t, err)
	assert.Equal(t, "anthropic", p)
	assert.NotNil(t, c)
}
