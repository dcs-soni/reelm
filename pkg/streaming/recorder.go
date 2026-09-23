package streaming

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"time"

	"github.com/dcs-soni/reelm/pkg/store"
)

// StreamRecorder records Server-Sent Events (SSE) from an upstream response
// while piping them immediately to the downstream client with zero buffering delay.
type StreamRecorder struct {
	chunks   []store.StreamChunk
	fullBody bytes.Buffer
	started  time.Time
	lastTime time.Time
}

// NewStreamRecorder instantiates a new SSE recorder.
func NewStreamRecorder() *StreamRecorder {
	now := time.Now()
	return &StreamRecorder{
		chunks:   make([]store.StreamChunk, 0),
		started:  now,
		lastTime: now,
	}
}

// PipeAndRecord reads the upstream SSE stream, writes it immediately to clientWriter,
// flushes after every event, and records chunk timestamps for subsequent deterministic replay.
func (r *StreamRecorder) PipeAndRecord(clientWriter http.ResponseWriter, upstreamBody io.Reader) error {
	flusher, _ := clientWriter.(http.Flusher)
	reader := bufio.NewReader(upstreamBody)

	var currentEvent bytes.Buffer

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			// Write immediately to downstream client
			_, writeErr := clientWriter.Write(line)
			if writeErr != nil {
				return writeErr
			}
			currentEvent.Write(line)
			r.fullBody.Write(line)

			// SSE event boundary is indicated by an empty line (\n or \r\n)
			trimmed := bytes.TrimRight(line, "\r\n")
			if len(trimmed) == 0 && currentEvent.Len() > 0 {
				if flusher != nil {
					flusher.Flush()
				}
				now := time.Now()
				delay := now.Sub(r.lastTime)
				r.lastTime = now

				r.chunks = append(r.chunks, store.StreamChunk{
					Data:  currentEvent.String(),
					Delay: delay,
				})
				currentEvent.Reset()
			}
		}

		if err != nil {
			if err == io.EOF {
				// Flush any remaining trailing data
				if currentEvent.Len() > 0 {
					if flusher != nil {
						flusher.Flush()
					}
					r.chunks = append(r.chunks, store.StreamChunk{
						Data:  currentEvent.String(),
						Delay: time.Since(r.lastTime),
					})
				}
				break
			}
			return err
		}
	}

	return nil
}

// Chunks returns all recorded chunks with their relative delays.
func (r *StreamRecorder) Chunks() []store.StreamChunk {
	return r.chunks
}

// FullBody returns the accumulated string of the entire stream.
func (r *StreamRecorder) FullBody() string {
	return r.fullBody.String()
}
