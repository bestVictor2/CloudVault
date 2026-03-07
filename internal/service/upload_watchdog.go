package service

import (
	"CloudVault/config"
	"CloudVault/internal/repo"
	"CloudVault/internal/storage"
	"CloudVault/model"
	"context"
	"log"
	"time"
)

// CleanupStaleUploadSessions removes stale upload sessions and their chunks.
func CleanupStaleUploadSessions(ctx context.Context, staleAfter time.Duration, limit int) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if storage.Default == nil {
		return 0, nil
	}
	if limit <= 0 {
		limit = 100
	}
	cutoff := time.Now().Add(-staleAfter)
	var sessions []model.UploadSession
	if err := repo.Db.
		Where("status = ? AND updated_at < ?", 0, cutoff).
		Order("updated_at asc").
		Limit(limit).
		Find(&sessions).Error; err != nil {
		return 0, err
	}
	cleaned := 0
	for _, session := range sessions {
		var chunks []model.FileChunk
		if err := repo.Db.Where("upload_id = ?", session.UploadID).Find(&chunks).Error; err != nil {
			continue
		}
		for _, c := range chunks {
			_ = storage.Default.RemoveObject(ctx, config.AppConfig.BucketName, c.ChunkPath)
		}
		_ = repo.Db.Where("upload_id = ?", session.UploadID).Delete(&model.FileChunk{}).Error
		_ = repo.Db.Delete(&model.UploadSession{}, session.ID).Error
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
