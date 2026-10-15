package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrDuplicateInboxMessage = errors.New("message has already been processed by consumer")

type InboxRepository interface {
	Record(ctx context.Context, consumerName string, messageID string, payloadHash string, now time.Time) error
	Exists(ctx context.Context, consumerName string, messageID string) (bool, error)
	GetPayloadHash(ctx context.Context, consumerName string, messageID string) (string, error)
}

type PgxInboxRepository struct {
	pool *pgxpool.Pool
}

func NewInboxRepository(pool *pgxpool.Pool) *PgxInboxRepository {
	return &PgxInboxRepository{pool: pool}
}

func (r *PgxInboxRepository) Record(ctx context.Context, consumerName string, messageID string, payloadHash string, now time.Time) error {
	executor := getExecutor(ctx, r.pool)

	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	query := `
		INSERT INTO inbox (consumer_name, message_id, payload_hash, status, created_at)
		VALUES ($1, $2, $3, 'PROCESSED', $4)
		ON CONFLICT (consumer_name, message_id) DO NOTHING
	`

	tag, err := executor.Exec(ctx, query, consumerName, messageID, payloadHash, now)
	if err != nil {
		if IsUniqueViolation(err) {
			return ErrDuplicateInboxMessage
		}
		return fmt.Errorf("failed to insert inbox record: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrDuplicateInboxMessage
	}

	return nil
}

func (r *PgxInboxRepository) Exists(ctx context.Context, consumerName string, messageID string) (bool, error) {
	executor := getExecutor(ctx, r.pool)

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM inbox
			WHERE consumer_name = $1 AND message_id = $2
		)
	`

	var exists bool
	err := executor.QueryRow(ctx, query, consumerName, messageID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check inbox existence: %w", err)
	}

	return exists, nil
}

func (r *PgxInboxRepository) GetPayloadHash(ctx context.Context, consumerName string, messageID string) (string, error) {
	executor := getExecutor(ctx, r.pool)

	query := `
		SELECT payload_hash
		FROM inbox
		WHERE consumer_name = $1 AND message_id = $2
	`

	var hash string
	err := executor.QueryRow(ctx, query, consumerName, messageID).Scan(&hash)
	if err != nil {
		return "", fmt.Errorf("failed to get inbox payload hash: %w", err)
	}

	return hash, nil
}
