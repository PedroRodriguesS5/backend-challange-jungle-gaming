package database

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"backend-challenge-go-jungle-gaming/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type LedgerRepository interface {
	Create(ctx context.Context, entry domain.WalletLedgerEntry) error
	ListByWalletID(ctx context.Context, walletID string, cursor string, limit int) ([]domain.WalletLedgerEntry, string, error)
	CalculateBalance(ctx context.Context, walletID string) (decimal.Decimal, int, error)
}

type PgxLedgerRepository struct {
	pool *pgxpool.Pool
}

func NewLedgerRepository(pool *pgxpool.Pool) *PgxLedgerRepository {
	return &PgxLedgerRepository{pool: pool}
}

func (r *PgxLedgerRepository) Create(ctx context.Context, entry domain.WalletLedgerEntry) error {
	executor := getExecutor(ctx, r.pool)

	query := `
		INSERT INTO wallet_ledger_entries (
			id, wallet_id, transaction_id, direction,
			amount, balance_before, balance_after, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := executor.Exec(
		ctx,
		query,
		entry.ID(),
		entry.WalletID(),
		entry.TransactionID(),
		string(entry.Direction()),
		entry.Amount().Amount(),
		entry.BalanceBefore().Amount(),
		entry.BalanceAfter().Amount(),
		entry.CreatedAt(),
	)
	if err != nil {
		if IsUniqueViolation(err) {
			return domain.NewDomainError(
				domain.FailureCodeIdempotencyConflict,
				"ledger entry already exists for transaction and wallet",
				err,
			)
		}
		return fmt.Errorf("failed to insert ledger entry: %w", err)
	}

	return nil
}

func (r *PgxLedgerRepository) ListByWalletID(
	ctx context.Context,
	walletID string,
	cursor string,
	limit int,
) ([]domain.WalletLedgerEntry, string, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	executor := getExecutor(ctx, r.pool)

	var (
		query string
		args  []any
	)

	if cursor != "" {
		cursorTime, cursorID, err := decodeCursor(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", err)
		}
		query = `
			SELECT wle.id, wle.wallet_id, wle.transaction_id, wle.direction,
			       wle.amount, wle.balance_before, wle.balance_after, wle.created_at,
			       w.currency
			FROM wallet_ledger_entries wle
			JOIN wallets w ON w.id = wle.wallet_id
			WHERE wle.wallet_id = $1
			  AND (wle.created_at < $2 OR (wle.created_at = $2 AND wle.id < $3))
			ORDER BY wle.created_at DESC, wle.id DESC
			LIMIT $4
		`
		args = []any{walletID, cursorTime, cursorID, limit}
	} else {
		query = `
			SELECT wle.id, wle.wallet_id, wle.transaction_id, wle.direction,
			       wle.amount, wle.balance_before, wle.balance_after, wle.created_at,
			       w.currency
			FROM wallet_ledger_entries wle
			JOIN wallets w ON w.id = wle.wallet_id
			WHERE wle.wallet_id = $1
			ORDER BY wle.created_at DESC, wle.id DESC
			LIMIT $2
		`
		args = []any{walletID, limit}
	}

	rows, err := executor.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query ledger entries: %w", err)
	}
	defer rows.Close()

	entries := make([]domain.WalletLedgerEntry, 0, limit)
	for rows.Next() {
		var (
			id           string
			wID          string
			txID         string
			directionStr string
			amountDec    decimal.Decimal
			beforeDec    decimal.Decimal
			afterDec     decimal.Decimal
			createdAt    time.Time
			currency     string
		)

		err := rows.Scan(
			&id,
			&wID,
			&txID,
			&directionStr,
			&amountDec,
			&beforeDec,
			&afterDec,
			&createdAt,
			&currency,
		)
		if err != nil {
			return nil, "", fmt.Errorf("failed to scan ledger entry: %w", err)
		}

		amount, err := domain.NewMoneyFromDecimal(amountDec, currency)
		if err != nil {
			return nil, "", err
		}
		before, err := domain.NewMoneyFromDecimal(beforeDec, currency)
		if err != nil {
			return nil, "", err
		}
		after, err := domain.NewMoneyFromDecimal(afterDec, currency)
		if err != nil {
			return nil, "", err
		}

		entry, err := domain.NewWalletLedgerEntry(
			id,
			wID,
			txID,
			domain.LedgerDirection(directionStr),
			amount,
			before,
			after,
			createdAt,
		)
		if err != nil {
			return nil, "", err
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("row error reading ledger entries: %w", err)
	}

	nextCursor := ""
	if len(entries) == limit {
		last := entries[len(entries)-1]
		nextCursor = encodeCursor(last.CreatedAt(), last.ID())
	}

	return entries, nextCursor, nil
}

func (r *PgxLedgerRepository) CalculateBalance(ctx context.Context, walletID string) (decimal.Decimal, int, error) {
	executor := getExecutor(ctx, r.pool)

	query := `
		SELECT COALESCE(SUM(CASE WHEN direction = 'CREDIT' THEN amount WHEN direction = 'DEBIT' THEN -amount ELSE 0 END), 0)::TEXT,
		       COUNT(*)
		FROM wallet_ledger_entries
		WHERE wallet_id = $1
	`

	var (
		calculated decimal.Decimal
		count      int
	)

	err := executor.QueryRow(ctx, query, walletID).Scan(&calculated, &count)
	if err != nil {
		return decimal.Zero, 0, fmt.Errorf("failed to calculate ledger balance: %w", err)
	}

	return calculated, count, nil
}

func encodeCursor(t time.Time, id string) string {
	raw := fmt.Sprintf("%s|%s", t.UTC().Format(time.RFC3339Nano), id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(cursor string) (time.Time, string, error) {
	bytes, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", err
	}

	parts := strings.SplitN(string(bytes), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", errors.New("malformed cursor")
	}

	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", err
	}

	return t, parts[1], nil
}
