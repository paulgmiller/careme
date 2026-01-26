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
var ErrAlreadyExists = errors.New("cache entry already exists")

type PutCondition uint8

const (
	PutUnconditional PutCondition = iota
	PutIfNoneMatch
	// PutIfMatch
)

type PutOptions struct {
	Condition PutCondition
	// IfMatch updates the entry only if the current ETag matches this value.
	// IfMatch string
}

func Unconditional() PutOptions {
	return PutOptions{Condition: PutUnconditional}
}

func IfNoneMatch() PutOptions {
	return PutOptions{Condition: PutIfNoneMatch}
}

type Cache interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Exists(ctx context.Context, key string) (bool, error)
	Put(ctx context.Context, key, value string, opts PutOptions) error
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
		if !info.IsDir() && strings.HasPrefix(path, prefix) {
			keys = append(keys, strings.TrimPrefix(path, prefix))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

func (fc *FileCache) Exists(_ context.Context, key string) (bool, error) {
	_, err := os.Stat(filepath.Join(fc.Dir, key))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (fc *FileCache) Get(_ context.Context, key string) (io.ReadCloser, error) {
	data, err := os.Open(filepath.Join(fc.Dir, key))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return data, nil
}

func (fc *FileCache) Put(_ context.Context, key, value string, opts PutOptions) error {
	fullPath := filepath.Join(fc.Dir, key)
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if opts.Condition == PutIfNoneMatch {
		f, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			if os.IsExist(err) {
				return ErrAlreadyExists
			}
			return err
		}
		defer f.Close()
		if _, err := f.WriteString(value); err != nil {
			return err
		}
		return nil
	}

	// TODO: IfMatch support (write only if etag matches).
	return os.WriteFile(fullPath, []byte(value), 0644)
}
