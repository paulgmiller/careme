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
	return os.WriteFile(filepath.Join(fc.Dir, key), []byte(value), 0644)
}
