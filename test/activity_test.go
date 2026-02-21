package test

import (
	"Go_Pan/internal/activity"
	"Go_Pan/internal/repo"
	"Go_Pan/internal/service"
	"Go_Pan/model"
	"context"
	"fmt"
	"testing"
	"time"
)

func cleanActivityTables(t *testing.T) {
	repo.Db.Exec("SET FOREIGN_KEY_CHECKS = 0")
	tables := []string{
		"user_activity_daily",
		"file_share",
		"file_chunk",
		"upload_session",
		"user_file",
		"file_object",
		"user_db",
	}
	for _, table := range tables {
		if err := repo.Db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clean %s table failed: %v", table, err)
		}
	}
	repo.Db.Exec("SET FOREIGN_KEY_CHECKS = 1")
}

func TestActivityAggregation(t *testing.T) {
	cleanActivityTables(t)

	user := &model.User{
		UserName: "activity_user",
		Password: "123456",
		Email:    "activity@test.com",
	}
	if err := service.CreateUser(user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	now := time.Now()
	seed := now.UnixNano()
	events := []*activity.Event{
		{EventID: fmt.Sprintf("e-upload-%d", seed), UserID: user.ID, Action: activity.ActionUpload, FileBytes: 10, OccurredAt: now},
		{EventID: fmt.Sprintf("e-share-%d", seed), UserID: user.ID, Action: activity.ActionShare, OccurredAt: now},
		{EventID: fmt.Sprintf("e-download-%d", seed), UserID: user.ID, Action: activity.ActionDownload, FileBytes: 6, OccurredAt: now},
		{EventID: fmt.Sprintf("e-delete-%d", seed), UserID: user.ID, Action: activity.ActionDelete, FileBytes: 4, OccurredAt: now},
	}
	for _, event := range events {
		if err := activity.ApplyEvent(context.Background(), event); err != nil {
			t.Fatalf("ApplyEvent failed: %v", err)
		}
	}

	// duplicate should be ignored by dedup key
	if err := activity.ApplyEvent(context.Background(), events[0]); err != nil {
		t.Fatalf("ApplyEvent duplicate failed: %v", err)
	}

	summary, err := activity.GetSummary(context.Background(), user.ID, 1)
	if err != nil {
		t.Fatalf("GetSummary failed: %v", err)
	}
	if len(summary) != 1 {
		t.Fatalf("expect 1 day summary, got %d", len(summary))
	}
	item := summary[0]
	if item.UploadCount != 1 || item.UploadBytes != 10 {
		t.Fatalf("unexpected upload stats: %+v", item)
	}
	if item.ShareCount != 1 {
		t.Fatalf("unexpected share count: %+v", item)
	}
	if item.DownloadCount != 1 || item.DownloadBytes != 6 {
		t.Fatalf("unexpected download stats: %+v", item)
	}
	if item.DeleteCount != 1 || item.DeleteBytes != 4 {
		t.Fatalf("unexpected delete stats: %+v", item)
	}
}
