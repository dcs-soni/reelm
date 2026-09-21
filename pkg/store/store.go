package store

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store defines the public API for cassette persistence and querying.
type Store interface {
	Save(cassette *Cassette) error
	LoadByHash(hash string) (*Cassette, error)
	LoadByNamespace(namespace string) ([]*Cassette, error)
	Delete(hash string) error
	List() ([]*CassetteMeta, error)
	Prune(olderThan time.Time) (int, error)
	RebuildIndex() error
	Close() error
}

// DiskStore implements Store using disk files and an embedded index.
type DiskStore struct {
	mu         sync.RWMutex
	baseDir    string
	serializer Serializer
	index      *Index
}

// NewDiskStore instantiates and initializes a DiskStore.
func NewDiskStore(baseDir string, format string) (*DiskStore, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base cassette directory: %w", err)
	}

	ser, err := GetSerializer(format)
	if err != nil {
		return nil, err
	}

	idx, err := OpenIndex(baseDir)
	if err != nil {
		return nil, err
	}

	ds := &DiskStore{
		baseDir:    baseDir,
		serializer: ser,
		index:      idx,
	}

	// Rebuild index in background or check if needed
	if err := ds.RebuildIndex(); err != nil {
		_ = idx.Close()
		return nil, fmt.Errorf("initial index build failed: %w", err)
	}

	return ds, nil
}

// resolveRelativePath computes the relative disk path for a cassette:
// [<namespace>/]<first_2_chars_of_hash>/<hash>.<ext>
func (s *DiskStore) resolveRelativePath(c *Cassette) string {
	prefix := ""
	if len(c.Hash) >= 2 {
		prefix = c.Hash[:2]
	} else {
		prefix = "xx"
	}

	fileName := c.Hash + s.serializer.Extension()
	if c.Namespace != "" {
		return filepath.Join(c.Namespace, prefix, fileName)
	}
	return filepath.Join(prefix, fileName)
}

// Save writes a cassette to disk and updates the index.
func (s *DiskStore) Save(cassette *Cassette) error {
	if cassette == nil {
		return fmt.Errorf("cannot save nil cassette")
	}
	if cassette.Hash == "" {
		return fmt.Errorf("cannot save cassette with empty hash")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	relPath := s.resolveRelativePath(cassette)
	fullPath := filepath.Join(s.baseDir, relPath)

	// Ensure directory exists
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", dir, err)
	}

	data, err := s.serializer.Marshal(cassette)
	if err != nil {
		return fmt.Errorf("failed to serialize cassette: %w", err)
	}

	// Atomic file write using temporary file
	tmpFile := fmt.Sprintf("%s.tmp-%d", fullPath, time.Now().UnixNano())
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temporary cassette file: %w", err)
	}

	if err := os.Rename(tmpFile, fullPath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to commit cassette file %q: %w", fullPath, err)
	}

	// Update index
	if err := s.index.Put(cassette.Hash, cassette.Namespace, relPath); err != nil {
		return fmt.Errorf("failed to index cassette: %w", err)
	}

	return nil
}

// LoadByHash loads a cassette matching the given hash.
func (s *DiskStore) LoadByHash(hash string) (*Cassette, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	relPath, err := s.index.GetPath(hash)
	if err != nil {
		return nil, err
	}

	fullPath := filepath.Join(s.baseDir, relPath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cassette file %q: %w", fullPath, err)
	}

	// Detect serializer from file extension (supports loading both YAML and JSON cassettes)
	ext := strings.ToLower(filepath.Ext(fullPath))
	var ser Serializer
	if ext == ".json" {
		ser = NewJSONSerializer()
	} else {
		ser = NewYAMLSerializer()
	}

	cassette, err := ser.Unmarshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize cassette %q: %w", fullPath, err)
	}

	return cassette, nil
}

// LoadByNamespace loads all cassettes in a namespace.
func (s *DiskStore) LoadByNamespace(namespace string) ([]*Cassette, error) {
	hashes, err := s.index.GetHashesByNamespace(namespace)
	if err != nil {
		return nil, err
	}

	results := make([]*Cassette, 0, len(hashes))
	for _, h := range hashes {
		c, err := s.LoadByHash(h)
		if err == nil && c != nil {
			results = append(results, c)
		}
	}

	return results, nil
}

// Delete removes a cassette from disk and index.
func (s *DiskStore) Delete(hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	relPath, err := s.index.GetPath(hash)
	if err != nil {
		return err
	}

	fullPath := filepath.Join(s.baseDir, relPath)
	_ = os.Remove(fullPath)

	return s.index.Delete(hash, "")
}

// List enumerates all cassettes in the store.
func (s *DiskStore) List() ([]*CassetteMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var metas []*CassetteMeta

	err := filepath.WalkDir(s.baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		var ser Serializer = NewYAMLSerializer()
		if ext == ".json" {
			ser = NewJSONSerializer()
		}

		c, err := ser.Unmarshal(data)
		if err != nil {
			return nil
		}

		metas = append(metas, &CassetteMeta{
			Hash:       c.Hash,
			Namespace:  c.Namespace,
			Provider:   c.Provider,
			Endpoint:   c.Endpoint,
			RecordedAt: c.RecordedAt,
			FilePath:   path,
		})
		return nil
	})

	return metas, err
}

// Prune deletes cassettes older than the given threshold.
func (s *DiskStore) Prune(olderThan time.Time) (int, error) {
	metas, err := s.List()
	if err != nil {
		return 0, err
	}

	count := 0
	for _, m := range metas {
		if m.RecordedAt.Before(olderThan) {
			if err := s.Delete(m.Hash); err == nil {
				count++
			}
		}
	}
	return count, nil
}

// RebuildIndex scans all cassette files and syncs the index.
func (s *DiskStore) RebuildIndex() error {
	return filepath.WalkDir(s.baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		var ser Serializer = NewYAMLSerializer()
		if ext == ".json" {
			ser = NewJSONSerializer()
		}

		c, err := ser.Unmarshal(data)
		if err != nil || c.Hash == "" {
			return nil
		}

		relPath, err := filepath.Rel(s.baseDir, path)
		if err != nil {
			return nil
		}

		return s.index.Put(c.Hash, c.Namespace, relPath)
	})
}

// Close closes the underlying index and resources.
func (s *DiskStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.index.Close()
}
