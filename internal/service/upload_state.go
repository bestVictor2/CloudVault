package service

import (
	"CloudVault/config"
	"CloudVault/internal/repo"
	"CloudVault/internal/storage"
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	uploadSessionKeyPrefix = "upload:session:"
	uploadChunksKeyPrefix  = "upload:chunks:"
	uploadChunkMetaPrefix  = "upload:chunkmeta:"
	uploadHashKeyPrefix    = "upload:hash:"
	uploadExpiryKey        = "upload:expiry"
)

type uploadSessionState struct {
	UploadID    string
	UserID      uint64
	FileHash    string
	FileName    string
	FileSize    int64
	ChunkSize   int64
	TotalChunks int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func buildUploadSessionKey(uploadID string) string {
	return uploadSessionKeyPrefix + uploadID
}

func buildUploadChunksKey(uploadID string) string {
	return uploadChunksKeyPrefix + uploadID
}

func buildUploadChunkMetaKey(uploadID string) string {
	return uploadChunkMetaPrefix + uploadID
}

func buildUploadHashKey(userID uint64, hash string) string {
	return fmt.Sprintf("%s%d:%s", uploadHashKeyPrefix, userID, hash)
}

func buildUploadExpiryMember(uploadID string, totalChunks int) string {
	return fmt.Sprintf("%s|%d", uploadID, totalChunks)
}

func parseUploadExpiryMember(member string) (string, int, bool) {
	parts := strings.Split(member, "|")
	if len(parts) != 2 {
		return "", 0, false
	}
	total, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, false
	}
	return parts[0], total, true
}

func getUploadSessionTTL() time.Duration {
	ttl := config.AppConfig.UploadSessionTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return ttl
}

func saveUploadSession(ctx context.Context, state *uploadSessionState) error {
	if repo.Redis == nil {
		return errors.New("redis not initialized")
	}
	if state == nil || strings.TrimSpace(state.UploadID) == "" {
		return errors.New("invalid upload session")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now()
	if state.CreatedAt.IsZero() {
		state.CreatedAt = now
	}
	state.UpdatedAt = now
	ttl := getUploadSessionTTL()

	values := map[string]interface{}{
		"upload_id":    state.UploadID,
		"user_id":      state.UserID,
		"file_hash":    state.FileHash,
		"file_name":    state.FileName,
		"file_size":    state.FileSize,
		"chunk_size":   state.ChunkSize,
		"total_chunks": state.TotalChunks,
		"created_at":   state.CreatedAt.Unix(),
		"updated_at":   state.UpdatedAt.Unix(),
	}

	pipe := repo.Redis.TxPipeline()
	pipe.HSet(ctx, buildUploadSessionKey(state.UploadID), values)
	pipe.Expire(ctx, buildUploadSessionKey(state.UploadID), ttl)
	pipe.Expire(ctx, buildUploadChunksKey(state.UploadID), ttl)
	pipe.Expire(ctx, buildUploadChunkMetaKey(state.UploadID), ttl)
	pipe.ZAdd(ctx, uploadExpiryKey, redis.Z{
		Score:  float64(now.Add(ttl).Unix()),
		Member: buildUploadExpiryMember(state.UploadID, state.TotalChunks),
	})
	if strings.TrimSpace(state.FileHash) != "" {
		pipe.Set(ctx, buildUploadHashKey(state.UserID, state.FileHash), state.UploadID, ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func touchUploadSession(ctx context.Context, state *uploadSessionState) error {
	if repo.Redis == nil {
		return errors.New("redis not initialized")
	}
	if state == nil || strings.TrimSpace(state.UploadID) == "" {
		return errors.New("invalid upload session")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	state.UpdatedAt = time.Now()
	ttl := getUploadSessionTTL()
	pipe := repo.Redis.TxPipeline()
	pipe.HSet(ctx, buildUploadSessionKey(state.UploadID), "updated_at", state.UpdatedAt.Unix())
	pipe.Expire(ctx, buildUploadSessionKey(state.UploadID), ttl)
	pipe.Expire(ctx, buildUploadChunksKey(state.UploadID), ttl)
	pipe.Expire(ctx, buildUploadChunkMetaKey(state.UploadID), ttl)
	pipe.ZAdd(ctx, uploadExpiryKey, redis.Z{
		Score:  float64(state.UpdatedAt.Add(ttl).Unix()),
		Member: buildUploadExpiryMember(state.UploadID, state.TotalChunks),
	})
	if strings.TrimSpace(state.FileHash) != "" {
		pipe.Expire(ctx, buildUploadHashKey(state.UserID, state.FileHash), ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func loadUploadSession(ctx context.Context, uploadID string) (*uploadSessionState, error) {
	if repo.Redis == nil {
		return nil, errors.New("redis not initialized")
	}
	uploadID = strings.TrimSpace(uploadID)
	if uploadID == "" {
		return nil, errors.New("upload_id required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	values, err := repo.Redis.HGetAll(ctx, buildUploadSessionKey(uploadID)).Result()
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	state := &uploadSessionState{
		UploadID: uploadID,
		FileHash: values["file_hash"],
		FileName: values["file_name"],
	}
	if v := values["user_id"]; v != "" {
		if parsed, err := strconv.ParseUint(v, 10, 64); err == nil {
			state.UserID = parsed
		}
	}
	if v := values["file_size"]; v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			state.FileSize = parsed
		}
	}
	if v := values["chunk_size"]; v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			state.ChunkSize = parsed
		}
	}
	if v := values["total_chunks"]; v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			state.TotalChunks = parsed
		}
	}
	if v := values["created_at"]; v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			state.CreatedAt = time.Unix(parsed, 0)
		}
	}
	if v := values["updated_at"]; v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			state.UpdatedAt = time.Unix(parsed, 0)
		}
	}
	return state, nil
}

func loadUploadSessionByHash(ctx context.Context, userID uint64, hash string) (*uploadSessionState, error) {
	if repo.Redis == nil {
		return nil, errors.New("redis not initialized")
	}
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, gorm.ErrRecordNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	uploadID, err := repo.Redis.Get(ctx, buildUploadHashKey(userID, hash)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, gorm.ErrRecordNotFound
		}
		return nil, err
	}
	return loadUploadSession(ctx, uploadID)
}

type chunkMeta struct {
	Size int64
	ETag string
}

func encodeChunkMeta(meta chunkMeta) string {
	return fmt.Sprintf("%d|%s", meta.Size, strings.TrimSpace(meta.ETag))
}

func decodeChunkMeta(raw string) (chunkMeta, bool) {
	parts := strings.SplitN(strings.TrimSpace(raw), "|", 2)
	if len(parts) != 2 {
		return chunkMeta{}, false
	}
	size, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return chunkMeta{}, false
	}
	return chunkMeta{
		Size: size,
		ETag: strings.TrimSpace(parts[1]),
	}, true
}

func markChunkUploaded(ctx context.Context, state *uploadSessionState, chunkIndex int, meta chunkMeta) error {
	if repo.Redis == nil {
		return errors.New("redis not initialized")
	}
	if state == nil {
		return errors.New("upload session missing")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	state.UpdatedAt = time.Now()
	ttl := getUploadSessionTTL()
	metaField := strconv.Itoa(chunkIndex)
	member := buildUploadExpiryMember(state.UploadID, state.TotalChunks)
	pipe := repo.Redis.TxPipeline()
	pipe.SetBit(ctx, buildUploadChunksKey(state.UploadID), int64(chunkIndex), 1)
	pipe.HSet(ctx, buildUploadChunkMetaKey(state.UploadID), metaField, encodeChunkMeta(meta))
	pipe.HSet(ctx, buildUploadSessionKey(state.UploadID), "updated_at", state.UpdatedAt.Unix())
	pipe.Expire(ctx, buildUploadSessionKey(state.UploadID), ttl)
	pipe.Expire(ctx, buildUploadChunksKey(state.UploadID), ttl)
	pipe.Expire(ctx, buildUploadChunkMetaKey(state.UploadID), ttl)
	pipe.ZAdd(ctx, uploadExpiryKey, redis.Z{
		Score:  float64(state.UpdatedAt.Add(ttl).Unix()),
		Member: member,
	})
	if strings.TrimSpace(state.FileHash) != "" {
		pipe.Expire(ctx, buildUploadHashKey(state.UserID, state.FileHash), ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func loadChunkMetaMap(ctx context.Context, uploadID string) (map[int]chunkMeta, error) {
	if repo.Redis == nil {
		return nil, errors.New("redis not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	values, err := repo.Redis.HGetAll(ctx, buildUploadChunkMetaKey(uploadID)).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[int]chunkMeta, len(values))
	for key, raw := range values {
		index, err := strconv.Atoi(strings.TrimSpace(key))
		if err != nil || index < 0 {
			continue
		}
		meta, ok := decodeChunkMeta(raw)
		if !ok {
			continue
		}
		out[index] = meta
	}
	return out, nil
}

func listUploadedChunkIndices(ctx context.Context, uploadID string, totalChunks int) ([]int, error) {
	if repo.Redis == nil {
		return nil, errors.New("redis not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if totalChunks <= 0 {
		return []int{}, nil
	}
	raw, err := repo.Redis.Get(ctx, buildUploadChunksKey(uploadID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return []int{}, nil
		}
		return nil, err
	}
	uploaded := make([]int, 0, totalChunks)
	for i := 0; i < totalChunks; i++ {
		byteIndex := i / 8
		if byteIndex >= len(raw) {
			break
		}
		bitIndex := uint(7 - (i % 8))
		if raw[byteIndex]&(1<<bitIndex) != 0 {
			uploaded = append(uploaded, i)
		}
	}
	return uploaded, nil
}

func countUploadedChunks(ctx context.Context, uploadID string) (int64, error) {
	if repo.Redis == nil {
		return 0, errors.New("redis not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return repo.Redis.BitCount(ctx, buildUploadChunksKey(uploadID), &redis.BitCount{}).Result()
}

func clearUploadState(ctx context.Context, state *uploadSessionState) error {
	if repo.Redis == nil {
		return errors.New("redis not initialized")
	}
	if state == nil || strings.TrimSpace(state.UploadID) == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	member := buildUploadExpiryMember(state.UploadID, state.TotalChunks)
	pipe := repo.Redis.TxPipeline()
	pipe.Del(ctx, buildUploadSessionKey(state.UploadID))
	pipe.Del(ctx, buildUploadChunksKey(state.UploadID))
	pipe.Del(ctx, buildUploadChunkMetaKey(state.UploadID))
	pipe.ZRem(ctx, uploadExpiryKey, member)
	if strings.TrimSpace(state.FileHash) != "" {
		pipe.Del(ctx, buildUploadHashKey(state.UserID, state.FileHash))
	}
	_, err := pipe.Exec(ctx)
	return err
}

func buildChunkObjectPath(uploadID string, chunkIndex int) string {
	return fmt.Sprintf("chunks/%s/%d", uploadID, chunkIndex)
}

// CleanupStaleUploadSessions removes stale upload sessions and their chunks.
func CleanupStaleUploadSessions(ctx context.Context, staleAfter time.Duration, limit int) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if storage.Default == nil {
		return 0, nil
	}
	if repo.Redis == nil {
		return 0, nil
	}
	if limit <= 0 {
		limit = 100
	}
	cutoff := time.Now().Add(-staleAfter).Unix()
	members, err := repo.Redis.ZRangeByScore(ctx, uploadExpiryKey, &redis.ZRangeBy{
		Min:    "-inf",
		Max:    strconv.FormatInt(cutoff, 10),
		Offset: 0,
		Count:  int64(limit),
	}).Result()
	if err != nil {
		return 0, err
	}
	cleaned := 0
	for _, member := range members {
		uploadID, total, ok := parseUploadExpiryMember(member)
		if !ok {
			continue
		}
		state, err := loadUploadSession(ctx, uploadID)
		if err == nil && state != nil {
			total = state.TotalChunks
		}
		if total > 0 {
			for i := 0; i < total; i++ {
				_ = storage.Default.RemoveObject(ctx, config.AppConfig.BucketName, buildChunkObjectPath(uploadID, i))
			}
		}
		if state != nil {
			_ = clearUploadState(ctx, state)
		} else {
			_ = repo.Redis.Del(ctx, buildUploadSessionKey(uploadID)).Err()
			_ = repo.Redis.Del(ctx, buildUploadChunksKey(uploadID)).Err()
			_ = repo.Redis.Del(ctx, buildUploadChunkMetaKey(uploadID)).Err()
			_ = repo.Redis.ZRem(ctx, uploadExpiryKey, member).Err()
		}
		cleaned++
	}
	return cleaned, nil
}

// StartUploadSessionWatchdog periodically cleans stale upload sessions.
func StartUploadSessionWatchdog(ctx context.Context, interval, staleAfter time.Duration, limit int) {
	if interval <= 0 || staleAfter <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cleaned, err := CleanupStaleUploadSessions(ctx, staleAfter, limit)
				if err != nil {
					log.Printf("upload watchdog failed: %v", err)
					continue
				}
				if cleaned > 0 {
					log.Printf("upload watchdog cleaned %d stale sessions", cleaned)
				}
			}
		}
	}()
}
