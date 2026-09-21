package hasher

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

// NormalizeText normalizes whitespace and line endings for stable hashing.
func NormalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(s)
}

// CanonicalizeJSON ensures JSON strings have deterministic key order and compact formatting.
func CanonicalizeJSON(raw []byte) ([]byte, error) {
	var val interface{}
	if err := json.Unmarshal(raw, &val); err != nil {
		return raw, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(val); err != nil {
		return raw, err
	}
	// json.Encoder adds a trailing newline; remove it
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// CanonicalizeTools sorts tools alphabetically by function name for deterministic order.
func CanonicalizeTools(tools []Tool) []Tool {
	if len(tools) <= 1 {
		return tools
	}

	sorted := make([]Tool, len(tools))
	copy(sorted, tools)

	sort.SliceStable(sorted, func(i, j int) bool {
		nameI, _ := sorted[i].Function["name"].(string)
		nameJ, _ := sorted[j].Function["name"].(string)
		return nameI < nameJ
	})

	return sorted
}

// CleanParameters strips fields that do not alter the deterministic model generation
// (e.g., client user IDs, stream flags, telemetry headers).
func CleanParameters(params map[string]interface{}, ignoredKeys ...string) map[string]interface{} {
	if params == nil {
		return nil
	}

	ignoreSet := map[string]struct{}{
		"user":           {},
		"stream":         {},
		"stream_options": {},
		"n":              {},
		"seed":           {},
	}
	for _, k := range ignoredKeys {
		ignoreSet[k] = struct{}{}
	}

	clean := make(map[string]interface{})
	for k, v := range params {
		if _, ignore := ignoreSet[k]; !ignore {
			clean[k] = v
		}
	}
	return clean
}
