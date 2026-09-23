package streaming_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dcs-soni/reelm/pkg/store"
	"github.com/dcs-soni/reelm/pkg/streaming"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type flusherRecorder struct {
	*httptest.ResponseRecorder
	flushedCount int
}

func newFlusherRecorder() *flusherRecorder {
	return &flusherRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

func (f *flusherRecorder) Flush() {
	f.flushedCount++
}

func TestStreamRecorderPipeAndRecord(t *testing.T) {
	rawSSE := "data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n" +
		"data: {\"id\":\"2\",\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n" +
		"data: [DONE]\n\n"

	upstreamBody := strings.NewReader(rawSSE)
	clientWriter := newFlusherRecorder()

	recorder := streaming.NewStreamRecorder()
	err := recorder.PipeAndRecord(clientWriter, upstreamBody)
	require.NoError(t, err)

	// Verify client got the full stream in real time
	assert.Equal(t, rawSSE, clientWriter.Body.String())
	assert.Equal(t, 3, clientWriter.flushedCount)

	// Verify chunks recorded
	chunks := recorder.Chunks()
	require.Len(t, chunks, 3)
	assert.Contains(t, chunks[0].Data, "Hello")
	assert.Contains(t, chunks[1].Data, "world")
	assert.Contains(t, chunks[2].Data, "[DONE]")

	// Verify full body
	assert.Equal(t, rawSSE, recorder.FullBody())
}

func TestStreamReplayerInstantMode(t *testing.T) {
	chunks := []store.StreamChunk{
		{Data: "data: chunk1\n\n", Delay: 50 * time.Millisecond},
		{Data: "data: chunk2\n\n", Delay: 50 * time.Millisecond},
		{Data: "data: [DONE]\n\n", Delay: 10 * time.Millisecond},
	}

	replayer := streaming.NewStreamReplayer(streaming.ReplayModeInstant)
	writer := newFlusherRecorder()

	start := time.Now()
	err := replayer.Replay(writer, chunks)
	duration := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, "text/event-stream", writer.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", writer.Header().Get("Cache-Control"))
	assert.Equal(t, "data: chunk1\n\ndata: chunk2\n\ndata: [DONE]\n\n", writer.Body.String())
	assert.Equal(t, 3, writer.flushedCount)
	assert.Less(t, duration, 50*time.Millisecond, "Instant mode should execute in under 50ms ignoring recorded delays")
}

func TestStreamReplayerTimedMode(t *testing.T) {
	chunks := []store.StreamChunk{
		{Data: "data: chunk1\n\n", Delay: 15 * time.Millisecond},
		{Data: "data: chunk2\n\n", Delay: 15 * time.Millisecond},
	}

	replayer := streaming.NewStreamReplayer(streaming.ReplayModeTimed)
	writer := newFlusherRecorder()

	start := time.Now()
	err := replayer.Replay(writer, chunks)
	duration := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, "data: chunk1\n\ndata: chunk2\n\n", writer.Body.String())
	assert.GreaterOrEqual(t, duration, 25*time.Millisecond, "Timed mode should respect recorded chunk delays")
}

func TestStreamReplayerRequiresFlusher(t *testing.T) {
	type nonFlusherWriter struct {
		http.ResponseWriter
	}

	replayer := streaming.NewStreamReplayer(streaming.ReplayModeInstant)
	err := replayer.Replay(nonFlusherWriter{httptest.NewRecorder()}, []store.StreamChunk{{Data: "test"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "flushing")
}
