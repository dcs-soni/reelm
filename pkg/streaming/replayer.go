package streaming

import (
	"fmt"
	"net/http"
	"time"

	"github.com/dcs-soni/reelm/pkg/store"
)

// ReplayMode defines the timing policy for streaming replay.
type ReplayMode string

const (
	ReplayModeInstant ReplayMode = "instant" // zero delay between chunks (best for CI)
	ReplayModeTimed   ReplayMode = "timed"   // preserves original inter-chunk delays (best for local dev)
)

// StreamReplayer replays recorded SSE chunks to an http.ResponseWriter.
type StreamReplayer struct {
	mode     ReplayMode
	maxDelay time.Duration
}

// NewStreamReplayer creates a new StreamReplayer.
func NewStreamReplayer(mode ReplayMode) *StreamReplayer {
	if mode == "" {
		mode = ReplayModeInstant
	}
	return &StreamReplayer{
		mode:     mode,
		maxDelay: 500 * time.Millisecond, // cap delay to prevent CI timeouts
	}
}

// Replay writes SSE chunks sequentially to the client response writer with flushing.
func (sr *StreamReplayer) Replay(w http.ResponseWriter, chunks []store.StreamChunk) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("response writer does not support flushing")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for _, chunk := range chunks {
		if sr.mode == ReplayModeTimed && chunk.Delay > 0 {
			sleepDur := chunk.Delay
			if sleepDur > sr.maxDelay {
				sleepDur = sr.maxDelay
			}
			time.Sleep(sleepDur)
		}

		if _, err := w.Write([]byte(chunk.Data)); err != nil {
			return err
		}
		flusher.Flush()
	}

	return nil
}
