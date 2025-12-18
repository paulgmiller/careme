package cache

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrNotFound = errors.New("cache entry not found")

type Cache interface {
	Get(key string) (io.ReadCloser, error)
	Set(key, value string) error
	//List(prefix string) ([]string, error)
}

type ListCache interface {
	Cache
	List(ctx context.Context, prefix string, token string) ([]string, error)
}

type FileCache struct {
	Dir string
}

var _ ListCache = (*FileCache)(nil)

func NewFileCache(dir string) *FileCache {
	return &FileCache{Dir: dir}
}

func (fc *FileCache) List(_ context.Context, prefix string, token string) ([]string, error) {
	var keys []string
	err := filepath.Walk(fc.Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			// Get relative path from cache directory
			relPath, err := filepath.Rel(fc.Dir, path)
			if err != nil {
				return err
			}
			if strings.HasPrefix(relPath, prefix) {
				keys = append(keys, strings.TrimPrefix(relPath, prefix))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
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
