package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"backend-challenge-go-jungle-gaming/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type WagerRepository interface {
	Create(ctx context.Context, tx *domain.WagerTransaction) error
	Update(ctx context.Context, tx *domain.WagerTransaction) error
	FindByID(ctx context.Context, id string) (*domain.WagerTransaction, error)
	FindByProviderAndExternalID(ctx context.Context, providerID string, externalID string) (*domain.WagerTransaction, error)
	FindByProviderAndIdempotencyKey(ctx context.Context, providerID string, idempotencyKey string) (*domain.WagerTransaction, error)
	FindPendingReferences(ctx context.Context, limit int) ([]*domain.WagerTransaction, error)
	HasSuccessfulReversal(ctx context.Context, providerID string, referenceExternalID string) (bool, error)
}

type PgxWagerRepository struct {
	pool *pgxpool.Pool
}

func NewWagerRepository(pool *pgxpool.Pool) *PgxWagerRepository {
	return &PgxWagerRepository{pool: pool}
}

func (r *PgxWagerRepository) Create(ctx context.Context, tx *domain.WagerTransaction) error {
	executor := getExecutor(ctx, r.pool)

	query := `
		INSERT INTO wager_transactions (
			id, external_id, provider_id, idempotency_key, payload_hash,
			wallet_id, player_id, round_id, game_id, kind,
			amount, currency, reference_external_id, status, failure_code,
			balance_after, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`

	var refID *string
	if tx.ReferenceExternalID() != "" {
		s := tx.ReferenceExternalID()
		refID = &s
	}

	var failureCode *string
	if tx.FailureCode() != "" {
		fc := tx.FailureCode()
		failureCode = &fc
	}

	var balAfter *decimal.Decimal
	if tx.BalanceAfter() != nil {
		dec := tx.BalanceAfter().Amount()
		balAfter = &dec
	}

	_, err := executor.Exec(
		ctx,
		query,
		tx.ID(),
		tx.ExternalID(),
		tx.ProviderID(),
		tx.IdempotencyKey(),
		tx.PayloadHash(),
		tx.WalletID(),
		tx.PlayerID(),
		nullIfEmpty(tx.RoundID()),
		nullIfEmpty(tx.GameID()),
		string(tx.Kind()),
		tx.Amount().Amount(),
		tx.Amount().Currency(),
		refID,
		string(tx.Status()),
		failureCode,
		balAfter,
		tx.CreatedAt(),
		tx.UpdatedAt(),
	)
	if err != nil {
		if IsUniqueViolation(err) {
			return domain.NewDomainError(
				domain.FailureCodeIdempotencyConflict,
				"transaction unique constraint violated",
				err,
			)
		}
		return fmt.Errorf("failed to insert wager transaction: %w", err)
	}

	return nil
}

func (r *PgxWagerRepository) Update(ctx context.Context, tx *domain.WagerTransaction) error {
	executor := getExecutor(ctx, r.pool)

	query := `
		UPDATE wager_transactions
		SET status = $1, failure_code = $2, balance_after = COALESCE($3, balance_after), updated_at = $4
		WHERE id = $5
	`

	var failureCode *string
	if tx.FailureCode() != "" {
		fc := tx.FailureCode()
		failureCode = &fc
	}

	var balAfter *decimal.Decimal
	if tx.BalanceAfter() != nil {
		dec := tx.BalanceAfter().Amount()
		balAfter = &dec
	}

	tag, err := executor.Exec(
		ctx,
		query,
		string(tx.Status()),
		failureCode,
		balAfter,
		tx.UpdatedAt(),
		tx.ID(),
	)
	if err != nil {
		return fmt.Errorf("failed to update wager transaction: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrTransactionNotFound
	}

	return nil
}

func (r *PgxWagerRepository) FindByID(ctx context.Context, id string) (*domain.WagerTransaction, error) {
	executor := getExecutor(ctx, r.pool)

	query := `
		SELECT id, external_id, provider_id, idempotency_key, payload_hash,
		       wallet_id, player_id, round_id, game_id, kind,
		       amount, currency, reference_external_id, status, failure_code,
		       balance_after, created_at, updated_at
		FROM wager_transactions
		WHERE id = $1
	`

	return r.scanTransaction(executor.QueryRow(ctx, query, id))
}

func (r *PgxWagerRepository) FindByProviderAndExternalID(ctx context.Context, providerID string, externalID string) (*domain.WagerTransaction, error) {
	executor := getExecutor(ctx, r.pool)

	query := `
		SELECT id, external_id, provider_id, idempotency_key, payload_hash,
		       wallet_id, player_id, round_id, game_id, kind,
		       amount, currency, reference_external_id, status, failure_code,
		       balance_after, created_at, updated_at
		FROM wager_transactions
		WHERE provider_id = $1 AND external_id = $2
	`

	return r.scanTransaction(executor.QueryRow(ctx, query, providerID, externalID))
}

func (r *PgxWagerRepository) FindByProviderAndIdempotencyKey(ctx context.Context, providerID string, idempotencyKey string) (*domain.WagerTransaction, error) {
	executor := getExecutor(ctx, r.pool)

	query := `
		SELECT id, external_id, provider_id, idempotency_key, payload_hash,
		       wallet_id, player_id, round_id, game_id, kind,
		       amount, currency, reference_external_id, status, failure_code,
		       balance_after, created_at, updated_at
		FROM wager_transactions
		WHERE provider_id = $1 AND idempotency_key = $2
	`

	return r.scanTransaction(executor.QueryRow(ctx, query, providerID, idempotencyKey))
}

func (r *PgxWagerRepository) FindPendingReferences(ctx context.Context, limit int) ([]*domain.WagerTransaction, error) {
	if limit <= 0 {
		limit = 100
	}

	executor := getExecutor(ctx, r.pool)

	query := `
		SELECT id, external_id, provider_id, idempotency_key, payload_hash,
		       wallet_id, player_id, round_id, game_id, kind,
		       amount, currency, reference_external_id, status, failure_code,
		       balance_after, created_at, updated_at
		FROM wager_transactions
		WHERE status = 'PENDING_REFERENCE'
		ORDER BY created_at ASC
		LIMIT $1
	`

	rows, err := executor.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending references: %w", err)
	}
	defer rows.Close()

	var list []*domain.WagerTransaction
	for rows.Next() {
		tx, err := r.scanTransactionRow(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, tx)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row error reading pending references: %w", err)
	}

	return list, nil
}

func (r *PgxWagerRepository) HasSuccessfulReversal(
	ctx context.Context,
	providerID string,
	referenceExternalID string,
) (bool, error) {
	executor := getExecutor(ctx, r.pool)

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM wager_transactions
			WHERE provider_id = $1
			  AND reference_external_id = $2
			  AND kind IN ('REFUND', 'ROLLBACK')
			  AND status = 'PROCESSED'
		)
	`

	var exists bool
	err := executor.QueryRow(ctx, query, providerID, referenceExternalID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check reversal existence: %w", err)
	}

	return exists, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (r *PgxWagerRepository) scanTransaction(row pgx.Row) (*domain.WagerTransaction, error) {
	tx, err := r.scanTransactionRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTransactionNotFound
		}
		return nil, err
	}
	return tx, nil
}

func (r *PgxWagerRepository) scanTransactionRow(scanner rowScanner) (*domain.WagerTransaction, error) {
	var (
		id              string
		externalID      string
		providerID      string
		idempotency     string
		payloadHash     string
		walletID        string
		playerID        string
		roundID         sql.NullString
		gameID          sql.NullString
		kindStr         string
		amountDec       decimal.Decimal
		currency        string
		refExternalID   sql.NullString
		statusStr       string
		failureCode     sql.NullString
		balanceAfterDec *decimal.Decimal
		createdAt       time.Time
		updatedAt       time.Time
	)

	err := scanner.Scan(
		&id,
		&externalID,
		&providerID,
		&idempotency,
		&payloadHash,
		&walletID,
		&playerID,
		&roundID,
		&gameID,
		&kindStr,
		&amountDec,
		&currency,
		&refExternalID,
		&statusStr,
		&failureCode,
		&balanceAfterDec,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, err
	}

	money, err := domain.NewMoneyFromDecimal(amountDec, currency)
	if err != nil {
		return nil, fmt.Errorf("failed to create money from decimal: %w", err)
	}

	var balanceAfter *domain.Money
	if balanceAfterDec != nil {
		m, err := domain.NewMoneyFromDecimal(*balanceAfterDec, currency)
		if err == nil {
			balanceAfter = &m
		}
	}

	return domain.RehydrateWagerTransaction(
		id,
		externalID,
		providerID,
		idempotency,
		payloadHash,
		walletID,
		playerID,
		roundID.String,
		gameID.String,
		domain.WagerKind(kindStr),
		money,
		refExternalID.String,
		domain.WagerStatus(statusStr),
		failureCode.String,
		balanceAfter,
		createdAt,
		updatedAt,
	)
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
