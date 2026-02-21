package activity

import (
	"Go_Pan/internal/mq"
	"Go_Pan/utils"
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const (
	ActionUpload   = "upload"
	ActionDelete   = "delete"
	ActionShare    = "share"
	ActionDownload = "download"
)

var validActions = map[string]struct{}{
	ActionUpload:   {},
	ActionDelete:   {},
	ActionShare:    {},
	ActionDownload: {},
}

// Event is the message format for activity aggregation.
type Event struct {
	EventID    string    `json:"event_id"`
	UserID     uint64    `json:"user_id"`
	Action     string    `json:"action"`
	FileID     uint64    `json:"file_id,omitempty"`
	FileBytes  int64     `json:"file_bytes,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

// Emit publishes an activity event and falls back to direct aggregation on publish failures.
func Emit(ctx context.Context, userID uint64, action string, fileID uint64, fileBytes int64) error {
	event := &Event{
		EventID:    utils.GetToken(),
		UserID:     userID,
		Action:     action,
		FileID:     fileID,
		FileBytes:  fileBytes,
		OccurredAt: time.Now(),
	}
	if err := Publish(ctx, event); err != nil {
		return ApplyEvent(ctx, event)
	}
	return nil
}

// Publish sends one event into RabbitMQ.
func Publish(ctx context.Context, event *Event) error {
	if event == nil {
		return fmt.Errorf("nil activity event")
	}
	if event.UserID == 0 {
		return fmt.Errorf("invalid user id")
	}
	if _, ok := validActions[event.Action]; !ok {
		return fmt.Errorf("invalid action %q", event.Action)
	}
	if event.EventID == "" {
		event.EventID = utils.GetToken()
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now()
	}
	if ctx == nil {
		ctx = context.Background()
	}

	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	publisher, err := mq.GetPublisher()
	if err != nil {
		return err
	}
	return publisher.PublishActivity(ctx, body)
}
