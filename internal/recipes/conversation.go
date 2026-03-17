package recipes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"careme/internal/cache"
)

const responseCachePrefix = "response/"

func responseKey(userID, originHash string) string {
	return fmt.Sprintf("%s%s/%s", responseCachePrefix, strings.TrimSpace(userID), strings.TrimSpace(originHash))
}

func (s *server) loadLastResponseIDForUser(ctx context.Context, userID, originHash string) (string, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(originHash) == "" {
		return "", nil
	}
	reader, err := s.Cache.Get(ctx, responseKey(userID, originHash))
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	defer func() { _ = reader.Close() }()

	buf, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buf)), nil
}

func (s *server) saveLastResponseIDForUser(ctx context.Context, userID, originHash, responseID string) error {
	userID = strings.TrimSpace(userID)
	originHash = strings.TrimSpace(originHash)
	responseID = strings.TrimSpace(responseID)
	if userID == "" || originHash == "" || responseID == "" {
		return nil
	}
	return s.Cache.Put(ctx, responseKey(userID, originHash), responseID, cache.Unconditional())
}
