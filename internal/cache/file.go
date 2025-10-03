package cache

import (
	"os"
	"path/filepath"
)

type Cache interface {
	Get(key string) (string, bool) //sigh need an error here.
	Set(key, value string) error
}

type FileCache struct {
	Dir string
}

var _ Cache = (*FileCache)(nil)

func NewFileCache(dir string) *FileCache {
	return &FileCache{Dir: dir}
}

func (fc *FileCache) Get(key string) (string, bool) {

	data, err := os.ReadFile(filepath.Join(fc.Dir, key))
	if err != nil {
		return "", false
	}
	return string(data), true
}

func (fc *FileCache) Set(key, value string) error {
	filePath := filepath.Join(fc.Dir, key)
	// Create parent directories if they don't exist
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}
	return os.WriteFile(filePath, []byte(value), 0644)
}
