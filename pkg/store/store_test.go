package store_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dcs-soni/reelm/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createSampleCassette(hash, namespace, body string) *store.Cassette {
	return &store.Cassette{
		Version:    store.CurrentCassetteVersion,
		Hash:       hash,
		Namespace:  namespace,
		Provider:   "openai",
		Endpoint:   "/v1/chat/completions",
		RecordedAt: time.Now().UTC(),
		Request: store.CassetteRequest{
			Method: "POST",
			Path:   "/v1/chat/completions",
			Body:   `{"model":"gpt-4o"}`,
		},
		Response: store.CassetteResponse{
			StatusCode: 200,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       body,
			Latency:    150 * time.Millisecond,
		},
	}
}

func TestStoreYAMLRoundtrip(t *testing.T) {
	tempDir := t.TempDir()
	s, err := store.NewDiskStore(tempDir, "yaml")
	require.NoError(t, err)
	defer s.Close()

	c := createSampleCassette("abcd1234ef5678", "test-ns", `{"choices":[{"message":{"content":"Hello YAML!"}}]}`)
	err = s.Save(c)
	require.NoError(t, err)

	loaded, err := s.LoadByHash("abcd1234ef5678")
	require.NoError(t, err)
	assert.Equal(t, c.Hash, loaded.Hash)
	assert.Equal(t, c.Namespace, loaded.Namespace)
	assert.Equal(t, c.Response.Body, loaded.Response.Body)
	assert.Equal(t, 200, loaded.Response.StatusCode)
}

func TestStoreJSONRoundtrip(t *testing.T) {
	tempDir := t.TempDir()
	s, err := store.NewDiskStore(tempDir, "json")
	require.NoError(t, err)
	defer s.Close()

	c := createSampleCassette("json1234ef5678", "", `{"choices":[{"message":{"content":"Hello JSON!"}}]}`)
	err = s.Save(c)
	require.NoError(t, err)

	loaded, err := s.LoadByHash("json1234ef5678")
	require.NoError(t, err)
	assert.Equal(t, c.Hash, loaded.Hash)
	assert.Equal(t, c.Response.Body, loaded.Response.Body)
}

func TestStoreNamespaceQuery(t *testing.T) {
	tempDir := t.TempDir()
	s, err := store.NewDiskStore(tempDir, "yaml")
	require.NoError(t, err)
	defer s.Close()

	c1 := createSampleCassette("hash1", "team-alpha", "response-1")
	c2 := createSampleCassette("hash2", "team-alpha", "response-2")
	c3 := createSampleCassette("hash3", "team-beta", "response-3")

	require.NoError(t, s.Save(c1))
	require.NoError(t, s.Save(c2))
	require.NoError(t, s.Save(c3))

	alphaList, err := s.LoadByNamespace("team-alpha")
	require.NoError(t, err)
	assert.Len(t, alphaList, 2)

	betaList, err := s.LoadByNamespace("team-beta")
	require.NoError(t, err)
	assert.Len(t, betaList, 1)
}

func TestStoreDeleteAndPrune(t *testing.T) {
	tempDir := t.TempDir()
	s, err := store.NewDiskStore(tempDir, "yaml")
	require.NoError(t, err)
	defer s.Close()

	oldTime := time.Now().Add(-48 * time.Hour)
	cOld := createSampleCassette("hashOld", "", "old response")
	cOld.RecordedAt = oldTime

	cNew := createSampleCassette("hashNew", "", "new response")

	require.NoError(t, s.Save(cOld))
	require.NoError(t, s.Save(cNew))

	// Prune cassettes older than 24h
	pruned, err := s.Prune(time.Now().Add(-24 * time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, pruned)

	// Old should not exist, new should exist
	_, err = s.LoadByHash("hashOld")
	assert.Error(t, err)

	loadedNew, err := s.LoadByHash("hashNew")
	require.NoError(t, err)
	assert.NotNil(t, loadedNew)

	// Direct delete
	err = s.Delete("hashNew")
	require.NoError(t, err)
	_, err = s.LoadByHash("hashNew")
	assert.Error(t, err)
}

func TestStoreIndexRebuild(t *testing.T) {
	tempDir := t.TempDir()
	s, err := store.NewDiskStore(tempDir, "yaml")
	require.NoError(t, err)

	c := createSampleCassette("hashToRebuild", "", "recoverable response")
	require.NoError(t, s.Save(c))
	require.NoError(t, s.Close())

	// Delete boltdb index file
	indexBolt := filepath.Join(tempDir, ".reelm_index.bolt")
	require.NoError(t, os.Remove(indexBolt))

	// Reopen store: it should automatically reconstruct the index from disk files
	s2, err := store.NewDiskStore(tempDir, "yaml")
	require.NoError(t, err)
	defer s2.Close()

	loaded, err := s2.LoadByHash("hashToRebuild")
	require.NoError(t, err)
	assert.Equal(t, c.Hash, loaded.Hash)
	assert.Equal(t, c.Response.Body, loaded.Response.Body)
}
