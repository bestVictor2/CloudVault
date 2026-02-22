package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	JWTSecret                 string
	DBHost                    string
	DBPort                    string
	DBUser                    string
	DBPass                    string
	DBName                    string
	DBNameTest                string
	RedisHost                 string
	RedisPort                 string
	RedisPassword             string
	RedisDB                   int
	MinioHost                 string
	MinioPort                 string
	MinioUsername             string
	MinioPassword             string
	BucketName                string
	BucketNameTest            string
	RabbitMQURL               string
	RabbitMQHost              string
	RabbitMQPort              string
	RabbitMQUser              string
	RabbitMQPass              string
	RabbitMQVhost             string
	RabbitMQPrefetch          int
	DownloadWorkerConcurrency int
	DownloadRate              float64
	DownloadBurst             int
	DownloadRetryMax          int
	DownloadRetryDelays       []time.Duration
	DownloadHTTPTimeout       time.Duration
	DownloadAllowPrivate      bool
	DownloadAllowedHosts      []string
	DownloadMaxBytes          int64
}

var AppConfig Config

// getEnv returns the environment value or a default.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func getEnvFloat(key string, defaultValue float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func getEnvBool(key string, defaultValue bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return defaultValue
	}
	switch value {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return defaultValue
	}
}

func getEnvInt64(key string, defaultValue int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func getEnvList(key string, defaultValue []string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return defaultValue
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		return defaultValue
	}
	return out
}

func getEnvDurationList(key string, defaultValue []time.Duration) []time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return defaultValue
	}
	parts := strings.Split(raw, ",")
	out := make([]time.Duration, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parsed, err := time.ParseDuration(part)
		if err != nil {
			return defaultValue
		}
		out = append(out, parsed)
	}
	if len(out) == 0 {
		return defaultValue
	}
	return out
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return defaultValue
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return defaultValue
	}
	return parsed
}

// InitConfig loads configuration and initializes sub-configs.
func InitConfig() {
	bucketNameTest := getEnv("BUCKET_NAME_TEST", "")
	if bucketNameTest == "" {
		bucketNameTest = getEnv("BUCKET_NAMETEST", "CloudVault-test")
	}
	rabbitHost := getEnv("RABBITMQ_HOST", "localhost")
	rabbitPort := getEnv("RABBITMQ_PORT", "5672")
	rabbitUser := getEnv("RABBITMQ_USER", "guest")
	rabbitPass := getEnv("RABBITMQ_PASSWORD", "guest")
	rabbitVhost := getEnv("RABBITMQ_VHOST", "/")
	rabbitURL := getEnv("RABBITMQ_URL", "")
	if rabbitURL == "" {
		rabbitURL = fmt.Sprintf(
			"amqp://%s:%s@%s:%s/%s",
			url.PathEscape(rabbitUser),
			url.PathEscape(rabbitPass),
			rabbitHost,
			rabbitPort,
			url.PathEscape(rabbitVhost),
		)
	}
	retryDelays := getEnvDurationList(
		"DOWNLOAD_RETRY_DELAYS",
		[]time.Duration{10 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute},
	)
	AppConfig = Config{
		JWTSecret:                 getEnv("JWT_SECRET", "l=ax+b"),
		DBHost:                    getEnv("DB_HOST", "localhost"),
		DBPort:                    getEnv("DB_PORT", "3306"),
		DBUser:                    getEnv("DB_USER", "root"),
		DBPass:                    getEnv("DB_PASS", "root"),
		DBName:                    getEnv("DB_NAME", "CloudVault"),
		DBNameTest:                getEnv("DB_NAME_TEST", "CloudVault_Test"),
		RedisHost:                 getEnv("REDIS_HOST", "localhost"),
		RedisPort:                 getEnv("REDIS_PORT", "6379"),
		RedisPassword:             getEnv("REDIS_PASSWORD", ""),
		RedisDB:                   0,
		MinioHost:                 getEnv("MINIO_HOST", "localhost"),
		MinioPort:                 getEnv("MINIO_PORT", "9000"),
		MinioUsername:             getEnv("MINIO_USERNAME", "minioadmin"),
		MinioPassword:             getEnv("MINIO_PASSWORD", "minioadmin"),
		BucketName:                getEnv("BUCKET_NAME", "netdisk"),
		BucketNameTest:            bucketNameTest,
		RabbitMQURL:               rabbitURL,
		RabbitMQHost:              rabbitHost,
		RabbitMQPort:              rabbitPort,
		RabbitMQUser:              rabbitUser,
		RabbitMQPass:              rabbitPass,
		RabbitMQVhost:             rabbitVhost,
		RabbitMQPrefetch:          getEnvInt("RABBITMQ_PREFETCH", 8),
		DownloadWorkerConcurrency: getEnvInt("DOWNLOAD_WORKER_CONCURRENCY", 4),
		DownloadRate:              getEnvFloat("DOWNLOAD_RATE", 2),
		DownloadBurst:             getEnvInt("DOWNLOAD_BURST", 4),
		DownloadRetryMax:          getEnvInt("DOWNLOAD_RETRY_MAX", 5),
		DownloadRetryDelays:       retryDelays,
		DownloadHTTPTimeout:       getEnvDuration("DOWNLOAD_HTTP_TIMEOUT", 30*time.Minute),
		DownloadAllowPrivate:      getEnvBool("DOWNLOAD_ALLOW_PRIVATE", false),
		DownloadAllowedHosts:      getEnvList("DOWNLOAD_ALLOW_HOSTS", nil),
		DownloadMaxBytes:          getEnvInt64("DOWNLOAD_MAX_BYTES", 0),
	}

	InitStorageConfig()
}
