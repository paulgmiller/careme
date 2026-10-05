package cache

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// CockroachCache stores values in a table named after its container.
// The caller owns the database connection and must close it when finished.
type CockroachCache struct {
	db    *sql.DB
	table string
	jsonb bool
}

type CockroachCacheOptions struct {
	// JSONB stores validated JSON documents instead of opaque bytes. Get returns
	// normalized JSON, which may change whitespace and object key ordering.
	// This option must match the value column type of an existing table.
	JSONB bool
}

var _ ListCache = (*CockroachCache)(nil)

// Close closes the underlying pool. When sharing a caller-supplied db between
// caches, close that db once after all caches have finished using it instead.
func (c *CockroachCache) Close() error {
	return c.db.Close()
}

// NewCockroachCache creates the cache table if necessary. db must use a
// PostgreSQL-compatible driver connected to CockroachDB.
func NewCockroachCache(ctx context.Context, db *sql.DB, container string, opts CockroachCacheOptions) (*CockroachCache, error) {
	if container == "" || strings.ContainsRune(container, 0) {
		return nil, fmt.Errorf("CockroachDB cache container must be nonempty and contain no NUL bytes")
	}
	table := pgx.Identifier{container}.Sanitize()
	valueType := "BYTES"
	if opts.JSONB {
		valueType = "JSONB"
	}
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+table+` (
		key STRING PRIMARY KEY,
		value `+valueType+` NOT NULL
	)`)
	if err != nil {
		return nil, fmt.Errorf("create CockroachDB cache table: %w", err)
	}
	return &CockroachCache{db: db, table: table, jsonb: opts.JSONB}, nil
}

// Get buffers the entire value in memory before returning a reader; it does not
// stream bytes from CockroachDB.
func (c *CockroachCache) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	var value []byte
	column := "value"
	if c.jsonb {
		column = "value::STRING"
	}
	err := c.db.QueryRowContext(ctx, `SELECT `+column+` FROM `+c.table+` WHERE key = $1`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get CockroachDB cache entry: %w", err)
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}

func (c *CockroachCache) Exists(ctx context.Context, key string) (bool, error) {
	var exists bool
	err := c.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM `+c.table+` WHERE key = $1)`, key).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check CockroachDB cache entry: %w", err)
	}
	return exists, nil
}

func (c *CockroachCache) Put(ctx context.Context, key, value string, opts PutOptions) error {
	return c.PutReader(ctx, key, strings.NewReader(value), opts)
}

func (c *CockroachCache) PutReader(ctx context.Context, key string, reader io.Reader, opts PutOptions) error {
	value, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read CockroachDB cache value: %w", err)
	}
	var databaseValue any = value
	placeholder := "$2"
	if c.jsonb {
		databaseValue = string(value)
		placeholder = "$2::JSONB"
	}
	query := `INSERT INTO ` + c.table + ` (key, value) VALUES ($1, ` + placeholder + `)`
	if opts.Condition == PutIfNoneMatch {
		query += ` ON CONFLICT (key) DO NOTHING`
	} else {
		query += ` ON CONFLICT (key) DO UPDATE SET value = excluded.value`
	}
	result, err := c.db.ExecContext(ctx, query, key, databaseValue)
	if err != nil {
		return fmt.Errorf("put CockroachDB cache entry: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count CockroachDB cache writes: %w", err)
	}
	if opts.Condition == PutIfNoneMatch && count == 0 {
		return ErrAlreadyExists
	}
	return nil
}

// List returns every matching key with prefix removed. Like the other cache
// backends, it ignores token and does not paginate.
func (c *CockroachCache) List(ctx context.Context, prefix, _ string) ([]string, error) {
	pattern := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix) + "%"
	rows, err := c.db.QueryContext(ctx, `SELECT key FROM `+c.table+` WHERE key LIKE $1 ESCAPE '\'`, pattern)
	if err != nil {
		return nil, fmt.Errorf("list CockroachDB cache entries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan CockroachDB cache key: %w", err)
		}
		keys = append(keys, strings.TrimPrefix(key, prefix))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read CockroachDB cache keys: %w", err)
	}
	sort.Strings(keys)
	return keys, nil
}

func (c *CockroachCache) Delete(ctx context.Context, key string) error {
	result, err := c.db.ExecContext(ctx, `DELETE FROM `+c.table+` WHERE key = $1`, key)
	if err != nil {
		return fmt.Errorf("delete CockroachDB cache entry: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count CockroachDB cache deletes: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}
