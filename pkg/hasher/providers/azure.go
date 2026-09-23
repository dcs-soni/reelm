package providers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/dcs-soni/reelm/pkg/hasher"
)

// AzureCanonicalizer parses and canonicalizes Azure OpenAI API requests (/openai/deployments/{deployment}/chat/completions).
type AzureCanonicalizer struct {
	openAICanonicalizer *OpenAICanonicalizer
}

// NewAzureCanonicalizer returns a new AzureCanonicalizer instance.
func NewAzureCanonicalizer() *AzureCanonicalizer {
	return &AzureCanonicalizer{
		openAICanonicalizer: NewOpenAICanonicalizer(),
	}
}

// Canonicalize transforms an Azure OpenAI HTTP request into a canonicalized CanonicRequest.
func (c *AzureCanonicalizer) Canonicalize(req *http.Request, body []byte) (*hasher.CanonicRequest, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("empty request body")
	}

	canonic, err := c.openAICanonicalizer.Canonicalize(req, body)
	if err != nil {
		return nil, err
	}

	canonic.Provider = "azure"

	// Extract deployment name from URL path: /openai/deployments/{deployment}/chat/completions
	if req != nil && req.URL != nil {
		path := req.URL.Path
		if strings.Contains(path, "/deployments/") {
			parts := strings.Split(path, "/")
			for i, p := range parts {
				if p == "deployments" && i+1 < len(parts) {
					canonic.Model = parts[i+1]
					break
				}
			}
		}
	}

	return canonic, nil
}
