package recipes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"careme/internal/cache"
)

const conversationCachePrefix = "conversation/"

func conversationKey(userID, originHash string) string {
	return fmt.Sprintf("%s%s/%s", conversationCachePrefix, strings.TrimSpace(userID), strings.TrimSpace(originHash))
}

func (s *server) loadConversationForUser(ctx context.Context, userID, originHash string) (string, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(originHash) == "" {
		return "", nil
	}
	reader, err := s.Cache.Get(ctx, conversationKey(userID, originHash))
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	defer func() {
		_ = reader.Close()
	}()

	buf, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buf)), nil
}

func (s *server) saveConversationForUser(ctx context.Context, userID, originHash, conversationID string) error {
	userID = strings.TrimSpace(userID)
	originHash = strings.TrimSpace(originHash)
	conversationID = strings.TrimSpace(conversationID)
	if userID == "" || originHash == "" || conversationID == "" {
		return nil
	}
	if err := s.Cache.Put(ctx, conversationKey(userID, originHash), conversationID, cache.Unconditional()); err != nil {
		return err
	}
	return nil
}

func (s *server) resolveConversationIDForUser(ctx context.Context, userID, originHash, fallbackConversationID string) (string, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(originHash) == "" {
		return strings.TrimSpace(fallbackConversationID), nil
	}
	if existing, err := s.loadConversationForUser(ctx, userID, originHash); err != nil {
		return "", err
	} else if existing != "" {
		return existing, nil
	}

	if s.generator == nil {
		if strings.TrimSpace(fallbackConversationID) != "" {
			return strings.TrimSpace(fallbackConversationID), nil
		}
		return "", nil
	}

	conversationID, err := s.generator.StartConversation(ctx)
	if err != nil {
		if strings.TrimSpace(fallbackConversationID) != "" {
			return strings.TrimSpace(fallbackConversationID), nil
		}
		return "", err
	}
	if err := s.saveConversationForUser(ctx, userID, originHash, conversationID); err != nil {
		return "", err
	}
	return conversationID, nil
}
