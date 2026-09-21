package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	bbolt "go.etcd.io/bbolt"
)

const (
	bucketCassettes  = "cassettes"
	bucketNamespaces = "namespaces"
	indexFileName    = ".reelm_index.bolt"
)

// Index provides fast hash-to-filepath lookup backed by an embedded bbolt database.
type Index struct {
	mu sync.RWMutex
	db *bbolt.DB
}

// OpenIndex opens or creates a BoltDB index inside the specified base directory.
func OpenIndex(baseDir string) (*Index, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cassette directory: %w", err)
	}

	dbPath := filepath.Join(baseDir, indexFileName)
	db, err := bbolt.Open(dbPath, 0600, &bbolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open boltdb index at %q: %w", dbPath, err)
	}

	// Initialize buckets
	err = db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketCassettes)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketNamespaces)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize index buckets: %w", err)
	}

	return &Index{db: db}, nil
}

// Put records a mapping from hash to relative file path and namespace index.
func (idx *Index) Put(hash, namespace, relPath string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	return idx.db.Update(func(tx *bbolt.Tx) error {
		bCassettes := tx.Bucket([]byte(bucketCassettes))
		if err := bCassettes.Put([]byte(hash), []byte(relPath)); err != nil {
			return err
		}

		if namespace != "" {
			bNamespaces := tx.Bucket([]byte(bucketNamespaces))
			var hashes []string
			if existing := bNamespaces.Get([]byte(namespace)); existing != nil {
				_ = json.Unmarshal(existing, &hashes)
			}

			// Avoid duplicates
			found := false
			for _, h := range hashes {
				if h == hash {
					found = true
					break
				}
			}
			if !found {
				hashes = append(hashes, hash)
				data, err := json.Marshal(hashes)
				if err != nil {
					return err
				}
				if err := bNamespaces.Put([]byte(namespace), data); err != nil {
					return err
				}
			}
		}

		return nil
	})
}

// GetPath retrieves the relative file path for a given hash.
func (idx *Index) GetPath(hash string) (string, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var relPath string
	err := idx.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketCassettes))
		val := b.Get([]byte(hash))
		if val == nil {
			return fmt.Errorf("cassette with hash %q not found in index", hash)
		}
		relPath = string(val)
		return nil
	})

	return relPath, err
}

// GetHashesByNamespace retrieves all hashes associated with a namespace.
func (idx *Index) GetHashesByNamespace(namespace string) ([]string, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var hashes []string
	err := idx.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketNamespaces))
		val := b.Get([]byte(namespace))
		if val == nil {
			return nil
		}
		return json.Unmarshal(val, &hashes)
	})

	return hashes, err
}

// Delete removes a hash from the index and namespace mappings.
func (idx *Index) Delete(hash, namespace string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	return idx.db.Update(func(tx *bbolt.Tx) error {
		bCassettes := tx.Bucket([]byte(bucketCassettes))
		if err := bCassettes.Delete([]byte(hash)); err != nil {
			return err
		}

		if namespace != "" {
			bNamespaces := tx.Bucket([]byte(bucketNamespaces))
			var hashes []string
			if val := bNamespaces.Get([]byte(namespace)); val != nil {
				_ = json.Unmarshal(val, &hashes)
				newHashes := make([]string, 0, len(hashes))
				for _, h := range hashes {
					if h != hash {
						newHashes = append(newHashes, h)
					}
				}
				if len(newHashes) == 0 {
					_ = bNamespaces.Delete([]byte(namespace))
				} else {
					data, _ := json.Marshal(newHashes)
					_ = bNamespaces.Put([]byte(namespace), data)
				}
			}
		}

		return nil
	})
}

// Close gracefully shuts down the BoltDB connection.
func (idx *Index) Close() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.db != nil {
		return idx.db.Close()
	}
	return nil
}
