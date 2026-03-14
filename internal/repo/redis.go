package repo

import (
	"CloudVault/config"
	"CloudVault/model"
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"log"
	"strings"
	"sync"
	"time"
)

var Redis *redis.Client

type RedisLock struct {
	rdb            *redis.Client
	key            string
	token          string
	ttl            time.Duration
	renewInterval  time.Duration
	renewCancel    context.CancelFunc
	renewDone      chan struct{}
	renewStartOnce sync.Once
}

// InitRedis initializes Redis client.
// Redis 客户端
func InitRedis() {
	RedisClient := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", config.AppConfig.RedisHost, config.AppConfig.RedisPort),
		Password: config.AppConfig.RedisPassword,
		DB:       config.AppConfig.RedisDB,
	})
	_, err := RedisClient.Ping(context.Background()).Result()
	if err != nil {
		log.Fatal("init redis fail", err)
	}
	log.Println("init redis success")
	Redis = RedisClient
}

// EnableKeyspaceNotifications enables Redis keyspace events.
func EnableKeyspaceNotifications(ctx context.Context) error {
	if Redis == nil {
		return errors.New("redis not initialized")
	}
	return Redis.ConfigSet(ctx, "notify-keyspace-events", "Ex").Err()
}

// NewRedisLock creates a Redis lock helper.
func NewRedisLock(rdb *redis.Client, key string, ttl time.Duration) *RedisLock {
	return &RedisLock{
		rdb:           rdb,
		key:           key,
		ttl:           ttl,
		renewInterval: ttl / 3,
	}
}

// Lock acquires a Redis-based lock.
func (l *RedisLock) Lock(ctx context.Context) error {
	token := uuid.NewString()
	ok, err := l.rdb.SetNX(ctx, l.key, token, l.ttl).Result()
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("lock is busy")
	}
	l.token = token
	l.startRenew()
	return nil
}

var unlockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

var renewScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`)

// Unlock releases a Redis-based lock.
func (l *RedisLock) Unlock(ctx context.Context) error {
	if l.token == "" {
		return nil
	}
	l.stopRenew()
	_, err := unlockScript.Run(
		ctx,
		l.rdb,
		[]string{l.key},
		l.token,
	).Result()
	return err
}

func (l *RedisLock) startRenew() {
	if l.ttl <= 0 || l.renewInterval <= 0 || l.token == "" {
		return
	}
	l.renewStartOnce.Do(func() {
		if l.renewInterval < 200*time.Millisecond {
			l.renewInterval = 200 * time.Millisecond
		}
		renewCtx, cancel := context.WithCancel(context.Background())
		l.renewCancel = cancel
		l.renewDone = make(chan struct{})
		go func() {
			ticker := time.NewTicker(l.renewInterval)
			defer ticker.Stop()
			defer close(l.renewDone)
			for {
				select {
				case <-renewCtx.Done():
					return
				case <-ticker.C:
					ok, err := renewScript.Run(
						renewCtx,
						l.rdb,
						[]string{l.key},
						l.token,
						int64(l.ttl/time.Millisecond),
					).Int()
					if err != nil {
						log.Printf("redis lock renew failed: %v", err)
						continue
					}
					if ok == 0 {
						return
					}
				}
			}
		}()
	})
}

func (l *RedisLock) stopRenew() {
	if l.renewCancel == nil {
		return
	}
	l.renewCancel()
	if l.renewDone != nil {
		<-l.renewDone
	}
}

// ListenRedisExpired listens for Redis expired events.
func ListenRedisExpired(ctx context.Context, rdb *redis.Client, ready chan<- struct{}) {
	channel := fmt.Sprintf("__keyevent@%d__:expired", config.AppConfig.RedisDB)
	pubsub := rdb.Subscribe(ctx, channel)
	defer pubsub.Close()
	_, err := pubsub.Receive(ctx)
	if err != nil {
		log.Printf("subscribe failed: %v", err)
		close(ready)
		return
	}
	close(ready)
	ch := pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			log.Printf("[listener] got message: %#v\n", msg)
			key := msg.Payload
			handleExpiredKey(ctx, key)
		}
	}
}

// handleExpiredKey dispatches expired-key handlers.
func handleExpiredKey(ctx context.Context, key string) {
	switch {
	case strings.HasPrefix(key, "share:"):
		handleShareExpired(ctx, key)
	default:
	}
}

// handleShareExpired marks a share as expired.
func handleShareExpired(ctx context.Context, key string) {
	shareID := strings.TrimPrefix(key, "share:")
	fmt.Println("share:")
	Db.Model(&model.FileShare{}).
		Where("share_id = ?", shareID).
		Update("status", 1)

	log.Println("share expired:", shareID)
}
