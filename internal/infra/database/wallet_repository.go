package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"backend-challenge-go-jungle-gaming/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type WalletRepository interface {
	Create(ctx context.Context, wallet *domain.Wallet) error
	FindByID(ctx context.Context, id string) (*domain.Wallet, error)
	FindByIDForUpdate(ctx context.Context, id string) (*domain.Wallet, error)
	FindByPlayerIDAndCurrency(ctx context.Context, playerID string, currency string) (*domain.Wallet, error)
	Update(ctx context.Context, wallet *domain.Wallet) error
}

type PgxWalletRepository struct {
	pool *pgxpool.Pool
}

func NewWalletRepository(pool *pgxpool.Pool) *PgxWalletRepository {
	return &PgxWalletRepository{pool: pool}
}

func (r *PgxWalletRepository) Create(ctx context.Context, wallet *domain.Wallet) error {
	executor := getExecutor(ctx, r.pool)

	query := `
		INSERT INTO wallets (id, player_id, currency, balance, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := executor.Exec(
		ctx,
		query,
		wallet.ID(),
		wallet.PlayerID(),
		wallet.Currency(),
		wallet.Balance().Amount(),
		wallet.Version(),
		wallet.CreatedAt(),
		wallet.UpdatedAt(),
	)
	if err != nil {
		if IsUniqueViolation(err) {
			return domain.NewDomainError(
				domain.FailureCodeIdempotencyConflict,
				"wallet already exists for player and currency",
				err,
			)
		}
		return fmt.Errorf("failed to insert wallet: %w", err)
	}

	return nil
}

func (r *PgxWalletRepository) FindByID(ctx context.Context, id string) (*domain.Wallet, error) {
	executor := getExecutor(ctx, r.pool)

	query := `
		SELECT id, player_id, currency, balance, version, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`

	return r.scanWallet(executor.QueryRow(ctx, query, id))
}

func (r *PgxWalletRepository) FindByIDForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	executor := getExecutor(ctx, r.pool)

	query := `
		SELECT id, player_id, currency, balance, version, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR NO KEY UPDATE
	`

	return r.scanWallet(executor.QueryRow(ctx, query, id))
}

func (r *PgxWalletRepository) FindByPlayerIDAndCurrency(ctx context.Context, playerID string, currency string) (*domain.Wallet, error) {
	executor := getExecutor(ctx, r.pool)

	query := `
		SELECT id, player_id, currency, balance, version, created_at, updated_at
		FROM wallets
		WHERE player_id = $1 AND currency = $2
	`

	return r.scanWallet(executor.QueryRow(ctx, query, playerID, currency))
}

func (r *PgxWalletRepository) Update(ctx context.Context, wallet *domain.Wallet) error {
	executor := getExecutor(ctx, r.pool)

	query := `
		UPDATE wallets
		SET balance = $1, version = $2, updated_at = $3
		WHERE id = $4
	`

	tag, err := executor.Exec(
		ctx,
		query,
		wallet.Balance().Amount(),
		wallet.Version(),
		wallet.UpdatedAt(),
		wallet.ID(),
	)
	if err != nil {
		return fmt.Errorf("failed to update wallet: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrWalletNotFound
	}

	return nil
}

func (r *PgxWalletRepository) scanWallet(row pgx.Row) (*domain.Wallet, error) {
	var (
		id        string
		playerID  string
		currency  string
		balance   decimal.Decimal
		version   int64
		createdAt time.Time
		updatedAt time.Time
	)

	err := row.Scan(&id, &playerID, &currency, &balance, &version, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("failed to scan wallet: %w", err)
	}

	money, err := domain.NewMoneyFromDecimal(balance, currency)
	if err != nil {
		return nil, fmt.Errorf("failed to create money from decimal: %w", err)
	}

	return domain.RehydrateWallet(id, playerID, money, version, createdAt, updatedAt)
}
