package worker

import (
	"CloudVault/config"
	"CloudVault/internal/activity"
	"CloudVault/internal/mq"
	"context"
	"encoding/json"
	"errors"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RunActivityWorker consumes activity events from RabbitMQ.
func RunActivityWorker(ctx context.Context) error {
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
		mq.QueueActivity,
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
				return errors.New("activity worker: delivery channel closed")
			}
			handleActivityMessage(ctx, delivery)
		}
	}
}

func handleActivityMessage(ctx context.Context, delivery amqp.Delivery) {
	var event activity.Event
	if err := json.Unmarshal(delivery.Body, &event); err != nil {
		log.Printf("activity worker: invalid message: %v", err)
		_ = delivery.Ack(false)
		return
	}
	if err := activity.ApplyEvent(ctx, &event); err != nil {
		log.Printf("activity worker: apply failed: %v", err)
		_ = delivery.Nack(false, true)
		return
	}
	_ = delivery.Ack(false)
}
