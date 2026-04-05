package task

import (
	"CloudVault/internal/dto"
	"CloudVault/internal/mq"
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// MergeMessage is the payload consumed by merge workers.
type MergeMessage struct {
	UploadID string                       `json:"upload_id"`
	UserID   uint64                       `json:"user_id"`
	UserName string                       `json:"user_name"`
	Request  dto.MultipartCompleteRequest `json:"request"`
	Attempt  int                          `json:"attempt"`
}

// EnqueueMergeTask publishes a merge task to RabbitMQ.
func EnqueueMergeTask(ctx context.Context, msg MergeMessage) error {
	msg.UploadID = strings.TrimSpace(msg.UploadID)
	if msg.UploadID == "" {
		return errors.New("upload_id required")
	}
	if msg.UserID == 0 {
		return errors.New("user_id required")
	}
	if strings.TrimSpace(msg.Request.UploadID) == "" {
		msg.Request.UploadID = msg.UploadID
	}
	publisher, err := mq.GetPublisher()
	if err != nil {
		return err
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return publisher.PublishMergeTask(ctx, body)
}
