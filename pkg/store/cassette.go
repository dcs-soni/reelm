package store

import (
	"time"
)

// CurrentCassetteVersion denotes the serialization schema version.
const CurrentCassetteVersion = 1

// Cassette represents a recorded interaction with an upstream LLM provider.
type Cassette struct {
	Version     int               `json:"version" yaml:"version"`
	Hash        string            `json:"hash" yaml:"hash"`
	Namespace   string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Provider    string            `json:"provider" yaml:"provider"`
	Endpoint    string            `json:"endpoint" yaml:"endpoint"`
	RecordedAt  time.Time         `json:"recorded_at" yaml:"recorded_at"`
	ExpiresAt   *time.Time        `json:"expires_at,omitempty" yaml:"expires_at,omitempty"`
	Request     CassetteRequest   `json:"request" yaml:"request"`
	Response    CassetteResponse  `json:"response" yaml:"response"`
	Metadata    map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// CassetteRequest encapsulates the inbound client request.
type CassetteRequest struct {
	Method  string            `json:"method" yaml:"method"`
	Path    string            `json:"path" yaml:"path"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Body    string            `json:"body" yaml:"body"`
}

// CassetteResponse encapsulates the recorded provider response.
type CassetteResponse struct {
	StatusCode int               `json:"status_code" yaml:"status_code"`
	Headers    map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Body       string            `json:"body" yaml:"body"`
	Chunks     []StreamChunk     `json:"chunks,omitempty" yaml:"chunks,omitempty"`
	Latency    time.Duration     `json:"latency" yaml:"latency"`
	IsStream   bool              `json:"is_stream,omitempty" yaml:"is_stream,omitempty"`
}

// StreamChunk represents a single chunk in an SSE streaming response.
type StreamChunk struct {
	Data  string        `json:"data" yaml:"data"`
	Delay time.Duration `json:"delay" yaml:"delay"`
}

// CassetteMeta contains lightweight metadata for listing and querying.
type CassetteMeta struct {
	Hash       string    `json:"hash"`
	Namespace  string    `json:"namespace,omitempty"`
	Provider   string    `json:"provider"`
	Endpoint   string    `json:"endpoint"`
	RecordedAt time.Time `json:"recorded_at"`
	FilePath   string    `json:"file_path"`
}
