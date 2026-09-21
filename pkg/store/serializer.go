package store

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Serializer converts cassettes to and from byte slices.
type Serializer interface {
	Marshal(c *Cassette) ([]byte, error)
	Unmarshal(data []byte) (*Cassette, error)
	Extension() string
}

// YAMLSerializer serializes cassettes into human-readable YAML.
type YAMLSerializer struct{}

// NewYAMLSerializer returns a YAML serializer.
func NewYAMLSerializer() *YAMLSerializer {
	return &YAMLSerializer{}
}

func (s *YAMLSerializer) Marshal(c *Cassette) ([]byte, error) {
	return yaml.Marshal(c)
}

func (s *YAMLSerializer) Unmarshal(data []byte) (*Cassette, error) {
	var c Cassette
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("failed to unmarshal YAML cassette: %w", err)
	}
	return &c, nil
}

func (s *YAMLSerializer) Extension() string {
	return ".yaml"
}

// JSONSerializer serializes cassettes into structured JSON.
type JSONSerializer struct{}

// NewJSONSerializer returns an indented JSON serializer.
func NewJSONSerializer() *JSONSerializer {
	return &JSONSerializer{}
}

func (s *JSONSerializer) Marshal(c *Cassette) ([]byte, error) {
	return json.MarshalIndent(c, "", "  ")
}

func (s *JSONSerializer) Unmarshal(data []byte) (*Cassette, error) {
	var c Cassette
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON cassette: %w", err)
	}
	return &c, nil
}

func (s *JSONSerializer) Extension() string {
	return ".json"
}

// GetSerializer returns the appropriate serializer for the given format name.
func GetSerializer(format string) (Serializer, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "yaml", "yml", "":
		return NewYAMLSerializer(), nil
	case "json":
		return NewJSONSerializer(), nil
	default:
		return nil, fmt.Errorf("unsupported cassette format %q: must be 'yaml' or 'json'", format)
	}
}
