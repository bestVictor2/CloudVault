package worker

import (
	"CloudVault/config"
	"CloudVault/internal/mq"
	"CloudVault/internal/service"
	"CloudVault/internal/task"
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/gorm"
)

type mergeDLQMessage struct {
	UploadID string    `json:"upload_id"`
	UserID   uint64    `json:"user_id"`
	Attempt  int       `json:"attempt"`
	Error    string    `json:"error"`
	FailedAt time.Time `json:"failed_at"`
}

// RunMergeWorker consumes merge tasks from RabbitMQ.
func RunMergeWorker(ctx context.Context) error {
	client, err := mq.Dial()
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.DeclareTopology(); err != nil {
		return err
	}

	prefetch := config.AppConfig.RabbitMQPrefetch
	if prefetch <= 0 {
		prefetch = 1
	}
	if err := client.Channel.Qos(prefetch, 0, false); err != nil {
		return err
	}

	deliveries, err := client.Channel.Consume(
		mq.QueueMerge,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("merge worker: delivery channel closed")
			}
			handleMergeMessage(ctx, client, delivery)
		}
	}
}

func handleMergeMessage(ctx context.Context, client *mq.Client, delivery amqp.Delivery) {
	var msg task.MergeMessage
	if err := json.Unmarshal(delivery.Body, &msg); err != nil {
		log.Printf("merge worker: invalid message: %v", err)
		_ = delivery.Ack(false)
		return
	}

	msg.UploadID = strings.TrimSpace(msg.UploadID)
	if msg.UploadID == "" {
		_ = delivery.Ack(false)
		return
	}
	if strings.TrimSpace(msg.Request.UploadID) == "" {
		msg.Request.UploadID = msg.UploadID
	}
	if strings.TrimSpace(msg.UserName) == "" && msg.UserID != 0 {
		if userName, err := service.FindUserNameById(msg.UserID); err == nil {
			msg.UserName = userName
		}
	}
	if strings.TrimSpace(msg.UserName) == "" {
		_ = delivery.Ack(false)
		return
	}

	if _, err := service.CompleteFileWithHash(ctx, msg.Request, msg.UserName); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = delivery.Ack(false)
			return
		}
		if shouldRetryMerge(err) {
			if err := scheduleMergeRetry(ctx, client, msg, err); err != nil {
				log.Printf("merge worker: retry schedule failed: %v", err)
				_ = delivery.Nack(false, true)
				return
			}
			_ = delivery.Ack(false)
			return
		}
		if err := publishMergeDLQ(ctx, client, msg, err); err != nil {
			log.Printf("merge worker: dlq publish failed: %v", err)
			_ = delivery.Nack(false, true)
			return
		}
	}

	_ = delivery.Ack(false)
}

func shouldRetryMerge(err error) bool {
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return true
	}
	nonRetryable := []string{
		"upload session forbidden",
		"upload session not found",
		"upload_id or file_hash required",
		"total_chunks mismatch",
		"file_hash mismatch",
		"file_size mismatch",
	}
	for _, key := range nonRetryable {
		if strings.Contains(msg, key) {
			return false
		}
	}
	return true
}

func scheduleMergeRetry(ctx context.Context, client *mq.Client, msg task.MergeMessage, procErr error) error {
	maxRetry := config.AppConfig.DownloadRetryMax
	if maxRetry < 0 {
		maxRetry = 0
	}
	nextAttempt := msg.Attempt + 1
	if maxRetry == 0 || nextAttempt > maxRetry {
		return publishMergeDLQ(ctx, client, msg, procErr)
	}
	msg.Attempt = nextAttempt
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	delay := pickRetryDelay(nextAttempt, config.AppConfig.DownloadRetryDelays)
	return client.PublishMergeRetry(ctx, body, delay)
}

func publishMergeDLQ(ctx context.Context, client *mq.Client, msg task.MergeMessage, procErr error) error {
	body, err := json.Marshal(mergeDLQMessage{
		UploadID: msg.UploadID,
		UserID:   msg.UserID,
		Attempt:  msg.Attempt,
		Error:    procErr.Error(),
		FailedAt: time.Now(),
	})
	if err != nil {
		return err
	}
	return client.PublishMergeDLQ(ctx, body)
}
