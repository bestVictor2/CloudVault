package service

import (
	"CloudVault/config"
	"CloudVault/internal/dto"
	"CloudVault/internal/repo"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

const aiHistoryKeyPrefix = "ai:history:user"

func buildAIHistoryKey(userID uint64) string {
	return fmt.Sprintf("%s:%d", aiHistoryKeyPrefix, userID)
}

func getAIHistoryLimit() int {
	limit := config.AppConfig.AIHistoryLimit
	if limit <= 0 {
		return 20
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func sanitizeAIHistory(history []dto.AIMessage) []dto.AIMessage {
	if len(history) == 0 {
		return []dto.AIMessage{}
	}
	out := make([]dto.AIMessage, 0, len(history))
	for _, msg := range history {
		role := normalizeAIRole(msg.Role)
		if role == "" || role == "system" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		out = append(out, dto.AIMessage{
			Role:    role,
			Content: content,
		})
	}
	return trimHistory(out, getAIHistoryLimit())
}

func loadAIHistoryFromStore(ctx context.Context, userID uint64) ([]dto.AIMessage, error) {
	if userID == 0 || repo.Redis == nil {
		return []dto.AIMessage{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	raw, err := repo.Redis.Get(ctx, buildAIHistoryKey(userID)).Result()
	if err == redis.Nil {
		return []dto.AIMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		return []dto.AIMessage{}, nil
	}
	var history []dto.AIMessage
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		_ = repo.Redis.Del(ctx, buildAIHistoryKey(userID)).Err()
		return []dto.AIMessage{}, nil
	}
	return sanitizeAIHistory(history), nil
}

func saveAIHistoryToStore(ctx context.Context, userID uint64, history []dto.AIMessage) error {
	if userID == 0 || repo.Redis == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	normalized := sanitizeAIHistory(history)
	body, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	ttl := config.AppConfig.AIHistoryTTL
	if ttl < 0 {
		ttl = 0
	}
	return repo.Redis.Set(ctx, buildAIHistoryKey(userID), string(body), ttl).Err()
}

func resolveAIConversationHistory(ctx context.Context, userID uint64, requestHistory []dto.AIMessage) []dto.AIMessage {
	if len(requestHistory) > 0 {
		return sanitizeAIHistory(requestHistory)
	}
	history, err := loadAIHistoryFromStore(ctx, userID)
	if err != nil {
		return []dto.AIMessage{}
	}
	return history
}

func appendConversation(history []dto.AIMessage, question, answer string) []dto.AIMessage {
	out := sanitizeAIHistory(history)

	question = strings.TrimSpace(question)
	if question != "" {
		if len(out) == 0 || !(out[len(out)-1].Role == "user" && strings.TrimSpace(out[len(out)-1].Content) == question) {
			out = append(out, dto.AIMessage{
				Role:    "user",
				Content: question,
			})
		}
	}

	answer = strings.TrimSpace(answer)
	if answer != "" {
		out = append(out, dto.AIMessage{
			Role:    "assistant",
			Content: answer,
		})
	}
	return trimHistory(out, getAIHistoryLimit())
}

func persistAIConversation(ctx context.Context, userID uint64, history []dto.AIMessage, question, answer string) {
	next := appendConversation(history, question, answer)
	_ = saveAIHistoryToStore(ctx, userID, next)
}

// GetAIHistory returns the latest persisted AI history for a user.
func GetAIHistory(ctx context.Context, userID uint64, limit int) ([]dto.AIMessage, error) {
	history, err := loadAIHistoryFromStore(ctx, userID)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(history) > limit {
		history = history[len(history)-limit:]
	}
	return history, nil
}

// ClearAIHistory clears persisted AI history for a user.
func ClearAIHistory(ctx context.Context, userID uint64) error {
	if userID == 0 || repo.Redis == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return repo.Redis.Del(ctx, buildAIHistoryKey(userID)).Err()
}
