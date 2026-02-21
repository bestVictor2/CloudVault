package mq

import (
	"Go_Pan/config"
	"context"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	ExchangeTasks    = "download.exchange"
	ExchangeRetry    = "download.retry.exchange"
	ExchangeDLQ      = "download.dlq.exchange"
	ExchangeActivity = "activity.exchange"

	QueueTasks    = "download.queue"
	QueueRetry    = "download.retry.queue"
	QueueDLQ      = "download.dlq.queue"
	QueueActivity = "activity.queue"

	RoutingTask     = "download"
	RoutingRetry    = "download.retry"
	RoutingDLQ      = "download.dlq"
	RoutingActivity = "activity"
)

type Client struct {
	Conn      *amqp.Connection //tcp
	Channel   *amqp.Channel    // AMQP
	publishMu sync.Mutex
}

var publisherMu sync.Mutex
var publisher *Client

func Dial() (*Client, error) {
	conn, err := amqp.Dial(config.AppConfig.RabbitMQURL)
	if err != nil {
		return nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &Client{Conn: conn, Channel: ch}, nil
}

func GetPublisher() (*Client, error) {
	publisherMu.Lock()
	defer publisherMu.Unlock()
	if publisher != nil {
		if !publisher.Conn.IsClosed() && !publisher.Channel.IsClosed() {
			return publisher, nil
		}
		publisher.Close()
		publisher = nil
	}
	client, err := Dial()
	if err != nil {
		return nil, err
	}
	if err := client.DeclareTopology(); err != nil {
		client.Close()
		return nil, err
	}
	publisher = client
	return publisher, nil
}

func (c *Client) Close() {
	if c == nil {
		return
	}
	if c.Channel != nil {
		_ = c.Channel.Close()
	}
	if c.Conn != nil {
		_ = c.Conn.Close()
	}
}

func (c *Client) DeclareTopology() error {
	if err := c.Channel.ExchangeDeclare(
		ExchangeTasks,
		"direct",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	if err := c.Channel.ExchangeDeclare(
		ExchangeRetry,
		"direct",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	if err := c.Channel.ExchangeDeclare(
		ExchangeDLQ,
		"direct",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	if err := c.Channel.ExchangeDeclare(
		ExchangeActivity,
		"direct",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	if _, err := c.Channel.QueueDeclare(
		QueueTasks,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	if _, err := c.Channel.QueueDeclare(
		QueueRetry,
		true,
		false,
		false,
		false,
		amqp.Table{
			"x-dead-letter-exchange":    ExchangeTasks,
			"x-dead-letter-routing-key": RoutingTask,
		},
	); err != nil {
		return err
	}
	if _, err := c.Channel.QueueDeclare(
		QueueDLQ,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	if _, err := c.Channel.QueueDeclare(
		QueueActivity,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	if err := c.Channel.QueueBind(
		QueueTasks,
		RoutingTask,
		ExchangeTasks,
		false,
		nil,
	); err != nil {
		return err
	}
	if err := c.Channel.QueueBind(
		QueueRetry,
		RoutingRetry,
		ExchangeRetry,
		false,
		nil,
	); err != nil {
		return err
	}
	if err := c.Channel.QueueBind(
		QueueDLQ,
		RoutingDLQ,
		ExchangeDLQ,
		false,
		nil,
	); err != nil {
		return err
	}
	if err := c.Channel.QueueBind(
		QueueActivity,
		RoutingActivity,
		ExchangeActivity,
		false,
		nil,
	); err != nil {
		return err
	}
	return nil
}

func (c *Client) PublishTask(ctx context.Context, body []byte) error {
	return c.publish(ctx, ExchangeTasks, RoutingTask, body, "")
}

func (c *Client) PublishRetry(ctx context.Context, body []byte, delay time.Duration) error {
	if delay < 0 {
		delay = 0
	}
	expiration := fmt.Sprintf("%d", delay.Milliseconds())
	return c.publish(ctx, ExchangeRetry, RoutingRetry, body, expiration)
}

func (c *Client) PublishDLQ(ctx context.Context, body []byte) error {
	return c.publish(ctx, ExchangeDLQ, RoutingDLQ, body, "")
}

func (c *Client) PublishActivity(ctx context.Context, body []byte) error {
	return c.publish(ctx, ExchangeActivity, RoutingActivity, body, "")
}

func (c *Client) publish(ctx context.Context, exchange, key string, body []byte, expiration string) error {
	c.publishMu.Lock()
	defer c.publishMu.Unlock()
	msg := amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
	}
	if expiration != "" {
		msg.Expiration = expiration
	}
	return c.Channel.PublishWithContext(
		ctx,
		exchange,
		key,
		false,
		false,
		msg,
	)
}
