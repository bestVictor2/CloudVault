package config

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
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
	UploadSessionTTL          time.Duration
	UploadWatchdogInterval    time.Duration
	UploadWatchdogBatch       int
	AIAPIBase                 string
	AIAPIKey                  string
	AIModel                   string
	AIChatCompletionsPath     string
	AIHTTPReferer             string
	AIXTitle                  string
	AIRequestTimeout          time.Duration
	AIMaxTokens               int
	AIHistoryLimit            int
	AISystemPrompt            string
	ESEnabled                 bool
	ESAddress                 string
	ESIndex                   string
	ESAPIKey                  string
	ESUsername                string
	ESPassword                string
	ESTimeout                 time.Duration
	ESContentMaxBytes         int64
	PreviewTranscodeEnabled   bool
	PreviewTranscodeFFmpeg    string
	PreviewTranscodeTimeout   time.Duration
	PreviewTranscodeMaxBytes  int64
}

var AppConfig Config
var loadEnvOnce sync.Once

func loadEnvFile() {
	loadEnvOnce.Do(func() {
		// Merge .env and .env.local (local overrides base), then inject only when
		// process env is empty. This keeps priority:
		// non-empty process env > .env.local > .env > defaults.
		merged := make(map[string]string)
		mergeDotEnv(merged, ".env")
		mergeDotEnv(merged, ".env.local")
		for key, value := range merged {
			if strings.TrimSpace(os.Getenv(key)) != "" {
				continue
			}
			if err := os.Setenv(key, value); err != nil {
				log.Printf("set env %s failed: %v", key, err)
			}
		}
	})
}

func mergeDotEnv(dst map[string]string, filename string) {
	values, err := godotenv.Read(filename)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("read %s failed: %v", filename, err)
		}
		return
	}
	for key, value := range values {
		dst[key] = value
	}
}

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
	loadEnvFile()

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
		DBNameTest:                getEnv("DB_NAME_TEST", "CloudVault_test"),
		RedisHost:                 getEnv("REDIS_HOST", "localhost"),
		RedisPort:                 getEnv("REDIS_PORT", "6379"),
		RedisPassword:             getEnv("REDIS_PASSWORD", ""),
		RedisDB:                   getEnvInt("REDIS_DB", 0),
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
		UploadSessionTTL:          getEnvDuration("UPLOAD_SESSION_TTL", 24*time.Hour),
		UploadWatchdogInterval:    getEnvDuration("UPLOAD_WATCHDOG_INTERVAL", 10*time.Minute),
		UploadWatchdogBatch:       getEnvInt("UPLOAD_WATCHDOG_BATCH", 100),
		AIAPIBase:                 strings.TrimRight(getEnv("AI_API_BASE", ""), "/"),
		AIAPIKey:                  getEnv("AI_API_KEY", ""),
		AIModel:                   getEnv("AI_MODEL", ""),
		AIChatCompletionsPath:     strings.TrimSpace(getEnv("AI_CHAT_COMPLETIONS_PATH", "")),
		AIHTTPReferer:             strings.TrimSpace(getEnv("AI_HTTP_REFERER", getEnv("APP_PUBLIC_URL", ""))),
		AIXTitle:                  strings.TrimSpace(getEnv("AI_X_TITLE", "CloudVault")),
		AIRequestTimeout:          getEnvDuration("AI_TIMEOUT", 30*time.Second),
		AIMaxTokens:               getEnvInt("AI_MAX_TOKENS", 512),
		AIHistoryLimit:            getEnvInt("AI_HISTORY_LIMIT", 20),
		AISystemPrompt:            getEnv("AI_SYSTEM_PROMPT", "You are a concise CloudVault assistant. Answer in Chinese for Chinese questions."),
		ESEnabled:                 getEnvBool("ES_ENABLED", false),
		ESAddress:                 strings.TrimRight(getEnv("ES_ADDRESS", ""), "/"),
		ESIndex:                   strings.TrimSpace(getEnv("ES_INDEX", "cloudvault_user_files")),
		ESAPIKey:                  strings.TrimSpace(getEnv("ES_API_KEY", "")),
		ESUsername:                strings.TrimSpace(getEnv("ES_USERNAME", "")),
		ESPassword:                getEnv("ES_PASSWORD", ""),
		ESTimeout:                 getEnvDuration("ES_TIMEOUT", 5*time.Second),
		ESContentMaxBytes:         getEnvInt64("ES_CONTENT_MAX_BYTES", 128*1024),
		PreviewTranscodeEnabled:   getEnvBool("PREVIEW_TRANSCODE_ENABLED", false),
		PreviewTranscodeFFmpeg:    strings.TrimSpace(getEnv("PREVIEW_TRANSCODE_FFMPEG", "ffmpeg")),
		PreviewTranscodeTimeout:   getEnvDuration("PREVIEW_TRANSCODE_TIMEOUT", 5*time.Minute),
		PreviewTranscodeMaxBytes:  getEnvInt64("PREVIEW_TRANSCODE_MAX_BYTES", 0),
	}

	InitStorageConfig()
}
