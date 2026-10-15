package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL        string
	HTTPPort           string
	KeycloakIssuerURL  string
	AWSEndpointURL     string
	AWSRegion          string
	AWSAccessKeyID     string
	AWSSecretAccessKey string
	SQSQueueURL        string
	SQSDLQURL          string
	SQSEventsQueueURL  string
}

func Load() (*Config, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		host := getEnvOrDefault("DB_HOST", "localhost")
		port := getEnvOrDefault("DB_PORT", "5432")
		user := getEnvOrDefault("DB_USER", "postgres")
		pass := getEnvOrDefault("DB_PASSWORD", "postgres")
		name := getEnvOrDefault("DB_NAME", "jungle_gaming")
		dbURL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, pass, host, port, name)
	}

	httpPort := getEnvOrDefault("HTTP_PORT", getEnvOrDefault("PORT", "8080"))

	return &Config{
		DatabaseURL:        dbURL,
		HTTPPort:           httpPort,
		KeycloakIssuerURL:  getEnvOrDefault("KEYCLOAK_ISSUER_URL", "http://localhost:8080/realms/jungle-gaming"),
		AWSEndpointURL:     getEnvOrDefault("AWS_ENDPOINT_URL", "http://localhost:4566"),
		AWSRegion:          getEnvOrDefault("AWS_REGION", "us-east-1"),
		AWSAccessKeyID:     getEnvOrDefault("AWS_ACCESS_KEY_ID", "dummy"),
		AWSSecretAccessKey: getEnvOrDefault("AWS_SECRET_ACCESS_KEY", "dummy"),
		SQSQueueURL:        getEnvOrDefault("SQS_QUEUE_URL", "http://localhost:4566/000000000000/wager-transactions.fifo"),
		SQSDLQURL:          getEnvOrDefault("SQS_DLQ_URL", "http://localhost:4566/000000000000/wager-transactions-dlq.fifo"),
		SQSEventsQueueURL:  getEnvOrDefault("SQS_EVENTS_QUEUE_URL", "http://localhost:4566/000000000000/wager-events.fifo"),
	}, nil
}

func getEnvOrDefault(key, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}
