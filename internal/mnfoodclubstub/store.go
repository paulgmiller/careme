package mnfoodclubstub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errNotFound = errors.New("transfer not found")

type transferStore interface {
	create(context.Context, record) (record, error)
	get(context.Context, string) (record, error)
	list(context.Context) ([]record, error)
	ping(context.Context) error
}

type sqlStore struct{ pool *pgxpool.Pool }

// OpenStore connects to the supplied database and creates the stub's transfer table.
func OpenStore(ctx context.Context, databaseURL string) (*sqlStore, error) {
	if databaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("invalid DATABASE_URL")
	}
	config.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("configure CockroachDB connection pool")
	}
	store := &sqlStore{pool: pool}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to CockroachDB: %w", err)
	}
	_, err = pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS mnfoodclub_stub_transfers (
 id TEXT PRIMARY KEY,
 idempotency_key TEXT NOT NULL UNIQUE,
 created_at TIMESTAMPTZ NOT NULL,
 request JSONB NOT NULL,
 response JSONB NOT NULL
 )`)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("create transfer table: %w", err)
	}
	return store, nil
}

func (s *sqlStore) Close() { s.pool.Close() }

func (s *sqlStore) ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// The unique key and single statement select one durable result across all replicas.
// A repeated key leaves the original request and response untouched, even on conflict.
func (s *sqlStore) create(ctx context.Context, candidate record) (record, error) {
	requestJSON, err := json.Marshal(candidate.Request)
	if err != nil {
		return record{}, fmt.Errorf("encode transfer request: %w", err)
	}
	responseJSON, err := json.Marshal(candidate.Response)
	if err != nil {
		return record{}, fmt.Errorf("encode transfer response: %w", err)
	}
	row := s.pool.QueryRow(ctx, `INSERT INTO mnfoodclub_stub_transfers
 (id, idempotency_key, created_at, request, response) VALUES ($1,$2,$3,$4,$5)
 ON CONFLICT (idempotency_key) DO UPDATE SET idempotency_key = excluded.idempotency_key
 RETURNING created_at, idempotency_key, request, response`, candidate.Response.TransferID, candidate.Key, candidate.Created, requestJSON, responseJSON)
	return scanRecord(row)
}

func (s *sqlStore) get(ctx context.Context, id string) (record, error) {
	return scanRecord(s.pool.QueryRow(ctx, `SELECT created_at, idempotency_key, request, response FROM mnfoodclub_stub_transfers WHERE id = $1`, id))
}

func (s *sqlStore) list(ctx context.Context) ([]record, error) {
	rows, err := s.pool.Query(ctx, `SELECT created_at, idempotency_key, request, response FROM mnfoodclub_stub_transfers ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list transfers: %w", err)
	}
	defer rows.Close()
	records := []record{}
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transfers: %w", err)
	}
	return records, nil
}

func scanRecord(row pgx.Row) (record, error) {
	var rec record
	var requestJSON, responseJSON []byte
	err := row.Scan(&rec.Created, &rec.Key, &requestJSON, &responseJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return record{}, errNotFound
	}
	if err != nil {
		return record{}, fmt.Errorf("read transfer: %w", err)
	}
	if err := json.Unmarshal(requestJSON, &rec.Request); err != nil {
		return record{}, fmt.Errorf("decode saved request: %w", err)
	}
	if err := json.Unmarshal(responseJSON, &rec.Response); err != nil {
		return record{}, fmt.Errorf("decode saved response: %w", err)
	}
	return rec, nil
}
