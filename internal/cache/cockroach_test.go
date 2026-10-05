package cache

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func mockCockroachCache(t *testing.T) (*CockroachCache, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS \"recipes\"").WillReturnResult(sqlmock.NewResult(0, 0))
	c, err := NewCockroachCache(context.Background(), db, "recipes", CockroachCacheOptions{})
	require.NoError(t, err)
	return c, mock
}

func TestCockroachCacheGet(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rows    *sqlmock.Rows
		err     error
		wantErr error
	}{
		{name: "binary", rows: sqlmock.NewRows([]string{"value"}).AddRow([]byte{0, 255, 1})},
		{name: "missing", err: sql.ErrNoRows, wantErr: ErrNotFound},
		{name: "database error", err: io.ErrUnexpectedEOF, wantErr: io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, mock := mockCockroachCache(t)
			q := mock.ExpectQuery("SELECT value FROM \"recipes\"").WithArgs("recipe/a")
			if tc.err != nil {
				q.WillReturnError(tc.err)
			} else {
				q.WillReturnRows(tc.rows)
			}
			r, err := c.Get(context.Background(), "recipe/a")
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.Nil(t, r)
				return
			}
			require.NoError(t, err)
			defer func() { require.NoError(t, r.Close()) }()
			value, err := io.ReadAll(r)
			require.NoError(t, err)
			require.Equal(t, []byte{0, 255, 1}, value)
		})
	}
}

func TestCockroachCacheWrites(t *testing.T) {
	for _, tc := range []struct {
		name     string
		opts     PutOptions
		conflict string
		count    int64
		dbErr    error
		wantErr  error
	}{
		{name: "overwrite", opts: Unconditional(), conflict: "DO UPDATE", count: 1},
		{name: "create", opts: IfNoneMatch(), conflict: "DO NOTHING", count: 1},
		{name: "exists", opts: IfNoneMatch(), conflict: "DO NOTHING", wantErr: ErrAlreadyExists},
		{name: "database error", opts: Unconditional(), conflict: "DO UPDATE", dbErr: io.ErrUnexpectedEOF, wantErr: io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, mock := mockCockroachCache(t)
			q := mock.ExpectExec("INSERT INTO \"recipes\".*ON CONFLICT .*"+tc.conflict).WithArgs("a", []byte("value"))
			if tc.dbErr != nil {
				q.WillReturnError(tc.dbErr)
			} else {
				q.WillReturnResult(sqlmock.NewResult(0, tc.count))
			}
			err := c.Put(context.Background(), "a", "value", tc.opts)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
	t.Run("reader failure does not write", func(t *testing.T) {
		c, _ := mockCockroachCache(t)
		require.ErrorIs(t, c.PutReader(context.Background(), "a", failingCacheReader{}, Unconditional()), io.ErrUnexpectedEOF)
	})
}

type failingCacheReader struct{}

func (failingCacheReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestCockroachCacheList(t *testing.T) {
	for _, prefix := range []string{"", "recipe/", `_%\é/`} {
		t.Run(prefix, func(t *testing.T) {
			c, mock := mockCockroachCache(t)
			pattern := "%"
			switch prefix {
			case "recipe/":
				pattern = "recipe/%"
			case `_%\é/`:
				pattern = `\_\%\\é/%`
			}
			mock.ExpectQuery("SELECT key FROM \"recipes\"").WithArgs(pattern).WillReturnRows(sqlmock.NewRows([]string{"key"}).AddRow(prefix + "z").AddRow(prefix + "a"))
			keys, err := c.List(context.Background(), prefix, "ignored")
			require.NoError(t, err)
			require.Equal(t, []string{"a", "z"}, keys)
		})
	}
	t.Run("row error returns no partial result", func(t *testing.T) {
		c, mock := mockCockroachCache(t)
		mock.ExpectQuery("SELECT key FROM \"recipes\"").WillReturnRows(sqlmock.NewRows([]string{"key"}).AddRow("a").AddRow("b").RowError(1, io.ErrUnexpectedEOF))
		keys, err := c.List(context.Background(), "", "")
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.Nil(t, keys)
	})
}

func TestCockroachCacheExistsAndDelete(t *testing.T) {
	c, mock := mockCockroachCache(t)
	ctx := context.Background()
	for _, exists := range []bool{true, false} {
		mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM "recipes" WHERE key = \$1\)`).WithArgs("a").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(exists))
		got, err := c.Exists(ctx, "a")
		require.NoError(t, err)
		require.Equal(t, exists, got)
	}
	for _, count := range []int64{1, 0} {
		mock.ExpectExec("DELETE FROM \"recipes\"").WithArgs("a").WillReturnResult(sqlmock.NewResult(0, count))
		err := c.Delete(ctx, "a")
		if count == 0 {
			require.ErrorIs(t, err, ErrNotFound)
		} else {
			require.NoError(t, err)
		}
	}
}

func TestCockroachCacheInitializationFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
	}()
	mock.ExpectExec("CREATE TABLE").WillReturnError(io.ErrUnexpectedEOF)
	c, err := NewCockroachCache(context.Background(), db, "recipes", CockroachCacheOptions{})
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.Nil(t, c)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCockroachCacheContainerTable(t *testing.T) {
	for _, tc := range []struct {
		container string
		table     string
	}{
		{container: "recipe-images", table: `"recipe-images"`},
		{container: "select", table: `"select"`},
		{container: "schema.recipes", table: `"schema.recipes"`},
		{container: `x"; DROP TABLE recipes; --`, table: `"x""; DROP TABLE recipes; --"`},
	} {
		t.Run(tc.container, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
			mock.ExpectExec(regexp.QuoteMeta(`CREATE TABLE IF NOT EXISTS ` + tc.table + ` ( key STRING PRIMARY KEY, value BYTES NOT NULL )`)).WillReturnResult(sqlmock.NewResult(0, 0))
			c, err := NewCockroachCache(context.Background(), db, tc.container, CockroachCacheOptions{})
			require.NoError(t, err)
			mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO `+tc.table+` (key, value) VALUES ($1, $2) ON CONFLICT (key) DO NOTHING`)).WithArgs("key", []byte("value")).WillReturnResult(sqlmock.NewResult(0, 1))
			require.NoError(t, c.Put(context.Background(), "key", "value", IfNoneMatch()))
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT value FROM ` + tc.table + ` WHERE key = $1`)).WithArgs("key").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow([]byte("value")))
			r, err := c.Get(context.Background(), "key")
			require.NoError(t, err)
			require.NoError(t, r.Close())
		})
	}
}

func TestCockroachCacheInvalidContainer(t *testing.T) {
	for _, container := range []string{"", "recipes\x00"} {
		t.Run(container, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
			c, err := NewCockroachCache(context.Background(), db, container, CockroachCacheOptions{})
			require.Error(t, err)
			require.Nil(t, c)
		})
	}
}

func TestCockroachCacheJSONB(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS "recipes" .*value JSONB NOT NULL`).WillReturnResult(sqlmock.NewResult(0, 0))
	c, err := NewCockroachCache(t.Context(), db, "recipes", CockroachCacheOptions{JSONB: true})
	require.NoError(t, err)
	for _, condition := range []PutOptions{Unconditional(), IfNoneMatch()} {
		conflict := "DO UPDATE SET value = excluded.value"
		if condition.Condition == PutIfNoneMatch {
			conflict = "DO NOTHING"
		}
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "recipes" (key, value) VALUES ($1, $2::JSONB) ON CONFLICT (key) `+conflict)).WithArgs("a", `{"title":"Soup"}`).WillReturnResult(sqlmock.NewResult(0, 1))
		require.NoError(t, c.Put(t.Context(), "a", `{"title":"Soup"}`, condition))
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT value::STRING FROM "recipes" WHERE key = $1`)).WithArgs("a").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(`{"title": "Soup"}`))
	r, err := c.Get(t.Context(), "a")
	require.NoError(t, err)
	defer func() { require.NoError(t, r.Close()) }()
	value, err := io.ReadAll(r)
	require.NoError(t, err)
	require.JSONEq(t, `{"title":"Soup"}`, string(value))
}

func TestCockroachCacheJSONBIntegration(t *testing.T) {
	url := os.Getenv("COCKROACH_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("COCKROACH_TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ctx := context.Background()
	container := "json-test-" + uuid.NewString()
	c, err := NewCockroachCache(ctx, db, container, CockroachCacheOptions{JSONB: true})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := db.ExecContext(ctx, `DROP TABLE `+pgx.Identifier{container}.Sanitize())
		require.NoError(t, err)
	})
	for _, value := range []string{`{"title":"Soup","servings":2}`, `null`, `[1,2]`, `"text"`} {
		require.NoError(t, c.PutReader(ctx, "a", strings.NewReader(value), Unconditional()))
		r, err := c.Get(ctx, "a")
		require.NoError(t, err)
		got, err := io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
		require.JSONEq(t, value, string(got))
	}
	require.ErrorIs(t, c.Put(ctx, "a", `{}`, IfNoneMatch()), ErrAlreadyExists)
	require.Error(t, c.Put(ctx, "a", "not JSON", Unconditional()))
	r, err := c.Get(ctx, "a")
	require.NoError(t, err)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	require.JSONEq(t, `"text"`, string(got))
	require.NoError(t, c.Put(ctx, "b", `{}`, IfNoneMatch()))
	keys, err := c.List(ctx, "", "")
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, keys)
	require.NoError(t, c.Delete(ctx, "a"))
	_, err = c.Get(ctx, "a")
	require.ErrorIs(t, err, ErrNotFound)
}

// Set COCKROACH_TEST_DATABASE_URL to a disposable database to exercise real SQL.
func TestCockroachCacheIntegration(t *testing.T) {
	url := os.Getenv("COCKROACH_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("COCKROACH_TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ctx := context.Background()
	container := "cache-test-" + uuid.NewString()
	c, err := NewCockroachCache(ctx, db, container, CockroachCacheOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS `+pgx.Identifier{container}.Sanitize()+`, `+pgx.Identifier{container + "-other"}.Sanitize())
		require.NoError(t, err)
	})
	t.Setenv("COCKROACH_DATABASE_URL", url)
	t.Setenv("AZURE_STORAGE_ACCOUNT_NAME", "unused")
	otherCache, err := EnsureCache(container + "-other")
	require.NoError(t, err)
	other := otherCache.(*CockroachCache)
	t.Cleanup(func() { require.NoError(t, other.Close()) })
	key := `_%\é/a`
	require.NoError(t, c.PutReader(ctx, key, strings.NewReader(string([]byte{0, 255, 1})), Unconditional()))
	r, err := c.Get(ctx, key)
	require.NoError(t, err)
	value, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	require.Equal(t, []byte{0, 255, 1}, value)
	require.NoError(t, other.Put(ctx, key, "other", IfNoneMatch()))
	require.NoError(t, other.Put(ctx, `_%\é/b`, "other", IfNoneMatch()))
	require.ErrorIs(t, c.Put(ctx, key, "lost", IfNoneMatch()), ErrAlreadyExists)
	require.NoError(t, c.Put(ctx, key, "updated", Unconditional()))
	r, err = c.Get(ctx, key)
	require.NoError(t, err)
	value, err = io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	require.Equal(t, "updated", string(value))
	require.NoError(t, c.Put(ctx, "unrelated", "", Unconditional()))
	keys, err := c.List(ctx, `_%\é/`, "")
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, keys)
	exists, err := c.Exists(ctx, key)
	require.NoError(t, err)
	require.True(t, exists)
	require.NoError(t, c.Delete(ctx, key))
	require.ErrorIs(t, c.Delete(ctx, key), ErrNotFound)
	_, err = c.Get(ctx, key)
	require.ErrorIs(t, err, ErrNotFound)
	exists, err = c.Exists(ctx, key)
	require.NoError(t, err)
	require.False(t, exists)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Go(func() { results <- c.Put(ctx, "race", "value", IfNoneMatch()) })
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else {
			require.True(t, errors.Is(err, ErrAlreadyExists), "%v", err)
		}
	}
	require.Equal(t, 1, winners)
}
