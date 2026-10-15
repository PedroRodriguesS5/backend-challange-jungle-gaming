package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type OutboxRecord struct {
	ID            int64
	EventID       string
	EventType     string
	AggregateID   string
	CorrelationID string
	CausationID   string
	Payload       json.RawMessage
	Status        string
	RetryCount    int
	NextRetryAt   time.Time
	PublishedAt   *time.Time
	CreatedAt     time.Time
}

type OutboxRepository interface {
	Save(ctx context.Context, record *OutboxRecord) error
	FetchPending(ctx context.Context, limit int) ([]*OutboxRecord, error)
	MarkPublished(ctx context.Context, id int64, now time.Time) error
	ScheduleRetry(ctx context.Context, id int64, nextRetryAt time.Time) error
	MarkFailed(ctx context.Context, id int64) error
}

type PgxOutboxRepository struct {
	pool *pgxpool.Pool
}

func NewOutboxRepository(pool *pgxpool.Pool) *PgxOutboxRepository {
	return &PgxOutboxRepository{pool: pool}
}

func (r *PgxOutboxRepository) Save(ctx context.Context, record *OutboxRecord) error {
	executor := getExecutor(ctx, r.pool)

	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	} else {
		record.CreatedAt = record.CreatedAt.UTC()
	}

	if record.NextRetryAt.IsZero() {
		record.NextRetryAt = record.CreatedAt
	} else {
		record.NextRetryAt = record.NextRetryAt.UTC()
	}

	if record.Status == "" {
		record.Status = "PENDING"
	}

	query := `
		INSERT INTO outbox (
			event_id, event_type, aggregate_id, correlation_id, causation_id,
			payload, status, retry_count, next_retry_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id
	`

	var causation *string
	if record.CausationID != "" {
		c := record.CausationID
		causation = &c
	}

	err := executor.QueryRow(
		ctx,
		query,
		record.EventID,
		record.EventType,
		record.AggregateID,
		record.CorrelationID,
		causation,
		record.Payload,
		record.Status,
		record.RetryCount,
		record.NextRetryAt,
		record.CreatedAt,
	).Scan(&record.ID)
	if err != nil {
		return fmt.Errorf("failed to insert outbox record: %w", err)
	}

	return nil
}

func (r *PgxOutboxRepository) FetchPending(ctx context.Context, limit int) ([]*OutboxRecord, error) {
	if limit <= 0 {
		limit = 50
	}

	executor := getExecutor(ctx, r.pool)

	query := `
		SELECT id, event_id, event_type, aggregate_id, correlation_id, causation_id,
		       payload, status, retry_count, next_retry_at, published_at, created_at
		FROM outbox
		WHERE status = 'PENDING' AND next_retry_at <= NOW()
		ORDER BY next_retry_at ASC, id ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`

	rows, err := executor.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pending outbox records: %w", err)
	}
	defer rows.Close()

	var records []*OutboxRecord
	for rows.Next() {
		var (
			rec         OutboxRecord
			causationID sql.NullString
			publishedAt sql.NullTime
		)

		err := rows.Scan(
			&rec.ID,
			&rec.EventID,
			&rec.EventType,
			&rec.AggregateID,
			&rec.CorrelationID,
			&causationID,
			&rec.Payload,
			&rec.Status,
			&rec.RetryCount,
			&rec.NextRetryAt,
			&publishedAt,
			&rec.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan outbox record: %w", err)
		}

		if causationID.Valid {
			rec.CausationID = causationID.String
		}
		if publishedAt.Valid {
			t := publishedAt.Time.UTC()
			rec.PublishedAt = &t
		}

		records = append(records, &rec)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row error reading outbox records: %w", err)
	}

	return records, nil
}

func (r *PgxOutboxRepository) MarkPublished(ctx context.Context, id int64, now time.Time) error {
	executor := getExecutor(ctx, r.pool)

	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	query := `
		UPDATE outbox
		SET status = 'PUBLISHED', published_at = $1
		WHERE id = $2
	`

	_, err := executor.Exec(ctx, query, now, id)
	if err != nil {
		return fmt.Errorf("failed to mark outbox record as published: %w", err)
	}

	return nil
}

func (r *PgxOutboxRepository) ScheduleRetry(ctx context.Context, id int64, nextRetryAt time.Time) error {
	executor := getExecutor(ctx, r.pool)

	query := `
		UPDATE outbox
		SET retry_count = retry_count + 1, next_retry_at = $1
		WHERE id = $2
	`

	_, err := executor.Exec(ctx, query, nextRetryAt.UTC(), id)
	if err != nil {
		return fmt.Errorf("failed to schedule retry for outbox record: %w", err)
	}

	return nil
}

func (r *PgxOutboxRepository) MarkFailed(ctx context.Context, id int64) error {
	executor := getExecutor(ctx, r.pool)

	query := `
		UPDATE outbox
		SET status = 'FAILED'
		WHERE id = $1
	`

	_, err := executor.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to mark outbox record as failed: %w", err)
	}

	return nil
}
