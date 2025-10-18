package cache

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

var ErrNotFound = errors.New("cache entry not found")

type Cache interface {
	Get(key string) (io.ReadCloser, error)
	Set(key, value string) error
}

type FileCache struct {
	Dir string
}

var _ Cache = (*FileCache)(nil)

func NewFileCache(dir string) *FileCache {
	return &FileCache{Dir: dir}
}

func (fc *FileCache) Get(key string) (io.ReadCloser, error) {

	data, err := os.Open(filepath.Join(fc.Dir, key))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return data, nil
}

func (fc *FileCache) Set(key, value string) error {
	fullPath := filepath.Join(fc.Dir, key)
	dir := filepath.Dir(fullPath)
	
	// Create parent directories if they don't exist
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	
	return os.WriteFile(fullPath, []byte(value), 0644)
}
