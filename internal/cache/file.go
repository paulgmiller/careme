package cache

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
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

func (fc *FileCache) List(_ context.Context, prefix string, _ string) ([]string, error) {
	var keys []string
	err := filepath.Walk(fc.Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relativePath, err := filepath.Rel(fc.Dir, path)
		if err != nil {
			return err
		}

		relativePath = filepath.ToSlash(relativePath)
		if strings.HasPrefix(relativePath, prefix) {
			keys = append(keys, strings.TrimPrefix(relativePath, prefix))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
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
		return writeIfNoneMatchAtomic(dir, fullPath, value)
	}

	// TODO: IfMatch support (write only if etag matches).
	return writeAtomic(dir, fullPath, value)
}

func writeAtomic(dir, targetPath, value string) error {
	tmpFile, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmpFile.WriteString(value); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	return os.Rename(tmpPath, targetPath)
}

func writeIfNoneMatchAtomic(dir, targetPath, value string) error {
	tmpFile, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmpFile.WriteString(value); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	if err := os.Link(tmpPath, targetPath); err != nil {
		if os.IsExist(err) {
			return ErrAlreadyExists
		}
		return err
	}

	return nil
}

func (fc *FileCache) Delete(_ context.Context, key string) error {
	err := os.Remove(filepath.Join(fc.Dir, key))
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	return nil
}
