package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dcs-soni/reelm/pkg/config"
	"github.com/dcs-soni/reelm/pkg/hasher"
	"github.com/dcs-soni/reelm/pkg/hasher/providers"
	"github.com/dcs-soni/reelm/pkg/store"
	"github.com/dcs-soni/reelm/pkg/streaming"
	"github.com/dcs-soni/reelm/pkg/telemetry"
	"github.com/rs/zerolog"
)

// Handler orchestrates request routing, canonicalization, hashing, replay, streaming, and recording.
type Handler struct {
	cfg            *config.Config
	store          store.Store
	hasher         hasher.Hasher
	registry       *providers.Registry
	httpClient     *http.Client
	logger         zerolog.Logger
	streamReplayer *streaming.StreamReplayer
	caManager      *CAManager
}

// NewHandler initializes a proxy HTTP handler with the given components.
func NewHandler(cfg *config.Config, s store.Store, h hasher.Hasher, reg *providers.Registry) *Handler {
	if reg == nil {
		reg = providers.DefaultRegistry()
	}
	if h == nil {
		h = hasher.NewDefaultHasher()
	}

	replayMode := streaming.ReplayMode(cfg.Streaming.ReplayMode)
	if replayMode == "" {
		replayMode = streaming.ReplayModeInstant
	}

	var caMgr *CAManager
	if cfg.TLS.Enabled {
		mgr, err := NewCAManager(cfg.TLS.CADir)
		if err == nil {
			caMgr = mgr
		}
	}

	return &Handler{
		cfg:      cfg,
		store:    s,
		hasher:   h,
		registry: reg,
		httpClient: &http.Client{
			Timeout: cfg.DefaultTimeout,
		},
		logger:         telemetry.RootLogger.With().Str("component", "proxy").Logger(),
		streamReplayer: streaming.NewStreamReplayer(replayMode),
		caManager:      caMgr,
	}
}

// SetCAManager allows setting or mocking the CA manager.
func (h *Handler) SetCAManager(mgr *CAManager) {
	h.caManager = mgr
}

// ServeHTTP handles every proxied request.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// 1. Intercept HTTP CONNECT method for transparent HTTPS tunneling
	if r.Method == http.MethodConnect {
		h.HandleConnect(w, r)
		return
	}

	reqID := r.Header.Get("X-Reelm-Request-Id")

	// 2. Health checks & internal utility routes
	if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
		return
	}

	// 3. Identify provider
	providerName, canonicalizer, err := h.registry.DetectProvider(r)
	if err != nil {
		h.logger.Warn().Str("path", r.URL.Path).Err(err).Msg("failed to detect provider")
		http.Error(w, fmt.Sprintf(`{"error":"unknown provider: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	reqLog := telemetry.RequestLogger(reqID, providerName, r.URL.Path, h.cfg.Mode)

	// 4. Buffer request body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		reqLog.Error().Err(err).Msg("failed to read request body")
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()
	// Restore body for any downstream reads
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// 5. Canonicalize and Hash request
	canonic, err := canonicalizer.Canonicalize(r, bodyBytes)
	if err != nil {
		reqLog.Error().Err(err).Msg("failed to canonicalize request")
		http.Error(w, fmt.Sprintf(`{"error":"canonicalization failed: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	hash, err := h.hasher.Hash(canonic)
	if err != nil {
		reqLog.Error().Err(err).Msg("failed to hash request")
		http.Error(w, `{"error":"failed to compute request hash"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("X-Reelm-Hash", hash)
	reqLog = reqLog.With().Str("hash", hash).Str("model", canonic.Model).Logger()

	// 6. Evaluate operating mode
	switch strings.ToLower(h.cfg.Mode) {
	case config.ModeReplay:
		h.handleReplay(w, r, hash, reqLog, start)
	case config.ModeRecord:
		h.handleRecord(w, r, providerName, hash, canonic, bodyBytes, reqLog, start)
	case config.ModeAuto:
		// Attempt replay first; on miss, record
		if cassette, err := h.store.LoadByHash(hash); err == nil && cassette != nil {
			h.serveCassette(w, cassette, "HIT", reqLog, start)
			return
		}
		h.handleRecord(w, r, providerName, hash, canonic, bodyBytes, reqLog, start)
	default:
		reqLog.Error().Str("mode", h.cfg.Mode).Msg("invalid operating mode")
		http.Error(w, `{"error":"invalid proxy operating mode"}`, http.StatusInternalServerError)
	}
}

// handleReplay attempts to fulfill the request strictly from cached cassettes.
func (h *Handler) handleReplay(w http.ResponseWriter, r *http.Request, hash string, log zerolog.Logger, start time.Time) {
	cassette, err := h.store.LoadByHash(hash)
	if err != nil || cassette == nil {
		w.Header().Set("X-Reelm-Cache", "MISS")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(fmt.Sprintf(
			`{"error":{"message":"Cassette not found in replay mode for hash %s","type":"reelm_cache_miss","hash":"%s"}}`,
			hash, hash,
		)))
		log.Warn().Dur("latency_ms", time.Since(start)).Msg("replay cache miss")
		return
	}

	h.serveCassette(w, cassette, "HIT", log, start)
}

// handleRecord proxies the request to the upstream LLM provider and saves the cassette.
func (h *Handler) handleRecord(w http.ResponseWriter, r *http.Request, providerName, hash string, canonic *hasher.CanonicRequest, bodyBytes []byte, log zerolog.Logger, start time.Time) {
	upstreamURL, err := h.resolveUpstreamURL(providerName, r.URL.Path, r.URL.RawQuery)
	if err != nil {
		log.Error().Err(err).Msg("failed to resolve upstream target URL")
		http.Error(w, fmt.Sprintf(`{"error":"upstream resolution failed: %s"}`, err.Error()), http.StatusBadGateway)
		return
	}

	// Create upstream request
	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		log.Error().Err(err).Msg("failed to build upstream request")
		http.Error(w, `{"error":"failed to build upstream request"}`, http.StatusInternalServerError)
		return
	}

	// Forward headers
	copyHeaders(r.Header, upReq.Header)

	// Forward to provider
	respStart := time.Now()
	resp, err := h.httpClient.Do(upReq)
	if err != nil {
		log.Error().Err(err).Str("upstream", upstreamURL).Msg("upstream request failed")
		http.Error(w, fmt.Sprintf(`{"error":"upstream request failed: %s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	upstreamLatency := time.Since(respStart)

	// Capture headers for cassette
	cassetteRespHeaders := make(map[string]string)
	for k, vals := range resp.Header {
		if len(vals) > 0 {
			cassetteRespHeaders[k] = vals[0]
		}
	}

	contentType := resp.Header.Get("Content-Type")
	isStream := strings.Contains(contentType, "text/event-stream")

	w.Header().Set("X-Reelm-Cache", "RECORDED")
	copyHeaders(resp.Header, w.Header())

	if isStream {
		// Real-time SSE streaming passthrough and recording
		w.WriteHeader(resp.StatusCode)

		recorder := streaming.NewStreamRecorder()
		if err := recorder.PipeAndRecord(w, resp.Body); err != nil {
			log.Error().Err(err).Msg("error while streaming SSE response to client")
		}

		cassette := &store.Cassette{
			Version:    store.CurrentCassetteVersion,
			Hash:       hash,
			Namespace:  canonic.Namespace,
			Provider:   providerName,
			Endpoint:   r.URL.Path,
			RecordedAt: time.Now().UTC(),
			Request: store.CassetteRequest{
				Method: r.Method,
				Path:   r.URL.Path,
				Body:   string(bodyBytes),
			},
			Response: store.CassetteResponse{
				StatusCode: resp.StatusCode,
				Headers:    cassetteRespHeaders,
				Body:       recorder.FullBody(),
				Chunks:     recorder.Chunks(),
				IsStream:   true,
				Latency:    upstreamLatency,
			},
		}

		if err := h.store.Save(cassette); err != nil {
			log.Error().Err(err).Msg("failed to save streaming cassette to store")
		} else {
			log.Info().Str("hash", hash).Int("chunks", len(recorder.Chunks())).Msg("recorded new streaming cassette")
		}
		return
	}

	// Non-streaming response
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error().Err(err).Msg("failed to read upstream response body")
		http.Error(w, `{"error":"failed to read upstream response body"}`, http.StatusBadGateway)
		return
	}

	cassette := &store.Cassette{
		Version:    store.CurrentCassetteVersion,
		Hash:       hash,
		Namespace:  canonic.Namespace,
		Provider:   providerName,
		Endpoint:   r.URL.Path,
		RecordedAt: time.Now().UTC(),
		Request: store.CassetteRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Body:   string(bodyBytes),
		},
		Response: store.CassetteResponse{
			StatusCode: resp.StatusCode,
			Headers:    cassetteRespHeaders,
			Body:       string(respBytes),
			Latency:    upstreamLatency,
		},
	}

	if err := h.store.Save(cassette); err != nil {
		log.Error().Err(err).Msg("failed to save cassette to store")
	} else {
		log.Info().Str("hash", hash).Msg("recorded new cassette")
	}

	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBytes)

	log.Info().
		Int("status", resp.StatusCode).
		Dur("upstream_ms", upstreamLatency).
		Dur("total_ms", time.Since(start)).
		Msg("proxied and recorded request")
}

// serveCassette streams a stored cassette response back to the client.
func (h *Handler) serveCassette(w http.ResponseWriter, c *store.Cassette, cacheStatus string, log zerolog.Logger, start time.Time) {
	w.Header().Set("X-Reelm-Cache", cacheStatus)
	w.Header().Set("X-Reelm-Hash", c.Hash)

	for k, v := range c.Response.Headers {
		if !isHopByHopHeader(k) {
			w.Header().Set(k, v)
		}
	}

	// Handle streaming replay
	if c.Response.IsStream && len(c.Response.Chunks) > 0 {
		statusCode := c.Response.StatusCode
		if statusCode == 0 {
			statusCode = http.StatusOK
		}
		w.WriteHeader(statusCode)
		if err := h.streamReplayer.Replay(w, c.Response.Chunks); err != nil {
			log.Error().Err(err).Msg("failed to replay streaming chunks")
		} else {
			log.Info().
				Str("cache", cacheStatus).
				Int("chunks", len(c.Response.Chunks)).
				Dur("latency_ms", time.Since(start)).
				Msg("served streaming response from cassette")
		}
		return
	}

	// Standard non-streaming replay
	statusCode := c.Response.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	w.WriteHeader(statusCode)

	_, _ = w.Write([]byte(c.Response.Body))

	log.Info().
		Str("cache", cacheStatus).
		Int("status", statusCode).
		Dur("latency_ms", time.Since(start)).
		Msg("served response from cassette")
}

// resolveUpstreamURL computes the full target URL for the selected provider.
func (h *Handler) resolveUpstreamURL(providerName, path, query string) (string, error) {
	for _, p := range h.cfg.Providers {
		if strings.EqualFold(p.Name, providerName) {
			base := strings.TrimRight(p.BaseURL, "/")
			full := base + path
			if query != "" {
				full += "?" + query
			}
			return full, nil
		}
	}

	// Provider-specific default fallbacks
	switch strings.ToLower(providerName) {
	case "openai":
		return "https://api.openai.com" + path, nil
	case "anthropic":
		return "https://api.anthropic.com" + path, nil
	case "gemini":
		return "https://generativelanguage.googleapis.com" + path, nil
	}

	return "", fmt.Errorf("no upstream configuration configured for provider %q", providerName)
}

func copyHeaders(src, dst http.Header) {
	for k, vv := range src {
		if isHopByHopHeader(k) {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func isHopByHopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
		"te", "trailers", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}
