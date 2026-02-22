package worker

import (
	"CloudVault/config"
	"CloudVault/internal/mq"
	"CloudVault/internal/repo"
	"CloudVault/internal/service"
	"CloudVault/internal/task"
	"CloudVault/model"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
)

type dlqMessage struct {
	TaskID   uint64    `json:"task_id"`
	Attempt  int       `json:"attempt"`
	Error    string    `json:"error"`
	FailedAt time.Time `json:"failed_at"`
}

// RunDownloadWorker consumes download tasks from RabbitMQ.
func RunDownloadWorker(ctx context.Context) error {
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
		mq.QueueTasks,
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

	concurrency := config.AppConfig.DownloadWorkerConcurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)

	burst := config.AppConfig.DownloadBurst
	if burst <= 0 {
		burst = 1
	}
	rateLimit := config.AppConfig.DownloadRate
	var limiter *rate.Limiter
	if rateLimit <= 0 {
		limiter = rate.NewLimiter(rate.Inf, burst)
	} else {
		limiter = rate.NewLimiter(rate.Limit(rateLimit), burst)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("download worker: delivery channel closed")
			}
			sem <- struct{}{}
			go func(d amqp.Delivery) {
				defer func() { <-sem }()
				handleDownloadMessage(ctx, client, limiter, d)
			}(delivery)
		}
	}
}

func handleDownloadMessage(ctx context.Context, client *mq.Client, limiter *rate.Limiter, delivery amqp.Delivery) {
	var msg task.DownloadMessage
	if err := json.Unmarshal(delivery.Body, &msg); err != nil {
		log.Printf("download worker: invalid message: %v", err)
		_ = delivery.Ack(false)
		return
	}

	if limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			_ = delivery.Nack(false, true)
			return
		}
	}

	if err := task.ProcessDownloadTask(ctx, msg.TaskID); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			_ = delivery.Nack(false, true)
			return
		}
		if shouldRetry(err) {
			if err := scheduleRetry(ctx, client, msg, err); err != nil {
				log.Printf("download worker: retry schedule failed: %v", err)
				_ = delivery.Nack(false, true)
				return
			}
		} else {
			if err := markFailed(ctx, client, msg, err); err != nil {
				log.Printf("download worker: mark failed failed: %v", err)
				_ = delivery.Nack(false, true)
				return
			}
		}
	}

	_ = delivery.Ack(false)
}

func shouldRetry(err error) bool {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false
	}
	var httpErr *service.HTTPStatusError
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode == http.StatusRequestTimeout || httpErr.StatusCode == http.StatusTooManyRequests {
			return true
		}
		return httpErr.StatusCode >= http.StatusInternalServerError
	}
	return true
}

func scheduleRetry(ctx context.Context, client *mq.Client, msg task.DownloadMessage, procErr error) error {
	maxRetry := config.AppConfig.DownloadRetryMax
	if maxRetry < 0 {
		maxRetry = 0
	}
	nextAttempt := msg.Attempt + 1
	if maxRetry == 0 || nextAttempt > maxRetry {
		return markFailed(ctx, client, msg, procErr)
	}

	delay := pickRetryDelay(nextAttempt, config.AppConfig.DownloadRetryDelays)
	nextRetryAt := time.Now().Add(delay)
	if err := repo.Db.Model(&model.DownloadTask{}).
		Where("id = ?", msg.TaskID).
		Updates(map[string]interface{}{
			"status":        "retrying",
			"error_msg":     procErr.Error(),
			"retry_count":   nextAttempt,
			"next_retry_at": &nextRetryAt,
		}).Error; err != nil {
		return err
	}

	msg.Attempt = nextAttempt
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return client.PublishRetry(ctx, body, delay)
}

func markFailed(ctx context.Context, client *mq.Client, msg task.DownloadMessage, procErr error) error {
	finishedAt := time.Now()
	if err := repo.Db.Model(&model.DownloadTask{}).
		Where("id = ?", msg.TaskID).
		Updates(map[string]interface{}{
			"status":      "failed",
			"error_msg":   procErr.Error(),
			"finished_at": &finishedAt,
		}).Error; err != nil {
		return err
	}

	dlq := dlqMessage{
		TaskID:   msg.TaskID,
		Attempt:  msg.Attempt,
		Error:    procErr.Error(),
		FailedAt: finishedAt,
	}
	body, err := json.Marshal(dlq)
	if err != nil {
		return err
	}
	if err := client.PublishDLQ(ctx, body); err != nil {
		log.Printf("download worker: dlq publish failed: %v", err)
	}
	return nil
}

func pickRetryDelay(attempt int, delays []time.Duration) time.Duration {
	if len(delays) == 0 {
		return 0
	}
	index := attempt - 1
	if index < 0 {
		index = 0
	}
	if index >= len(delays) {
		return delays[len(delays)-1]
	}
	return delays[index]
}

