//go:build integration

package integration_test

import (
	"context"
	"os"
	"testing"
	"time"

	"backend-challenge-go-jungle-gaming/internal/infra/database"
	"backend-challenge-go-jungle-gaming/internal/usecase"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TestSuite struct {
	Pool              *pgxpool.Pool
	TxManager         database.TxManager
	WalletRepo        database.WalletRepository
	WagerRepo         database.WagerRepository
	LedgerRepo        database.LedgerRepository
	InboxRepo         database.InboxRepository
	OutboxRepo        database.OutboxRepository
	CreateWalletUC    *usecase.CreateWalletUseCase
	ProcessWagerUC    *usecase.ProcessWagerUseCase
	GetWalletUC       *usecase.GetWalletUseCase
	ListLedgerUC      *usecase.ListLedgerUseCase
	ReconcileWalletUC *usecase.ReconcileWalletUseCase
}

func setupTestSuite(t *testing.T) *TestSuite {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/jungle_gaming?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, database.Config{
		DatabaseURL: dbURL,
		MaxConns:    10,
		MinConns:    2,
	})
	if err != nil {
		t.Fatalf("failed to connect to test database at %s: %v", dbURL, err)
	}
	t.Cleanup(func() {
		pool.Close()
	})

	runMigrations(t, pool)
	truncateTables(t, pool)

	txManager := database.NewPgxTxManager(pool)
	walletRepo := database.NewWalletRepository(pool)
	wagerRepo := database.NewWagerRepository(pool)
	ledgerRepo := database.NewLedgerRepository(pool)
	inboxRepo := database.NewInboxRepository(pool)
	outboxRepo := database.NewOutboxRepository(pool)

	createWalletUC := usecase.NewCreateWalletUseCase(txManager, walletRepo, wagerRepo, ledgerRepo, outboxRepo)
	processWagerUC := usecase.NewProcessWagerUseCase(txManager, walletRepo, wagerRepo, ledgerRepo, outboxRepo)
	getWalletUC := usecase.NewGetWalletUseCase(walletRepo)
	listLedgerUC := usecase.NewListLedgerUseCase(ledgerRepo)
	reconcileWalletUC := usecase.NewReconcileWalletUseCase(txManager, walletRepo, ledgerRepo)

	return &TestSuite{
		Pool:              pool,
		TxManager:         txManager,
		WalletRepo:        walletRepo,
		WagerRepo:         wagerRepo,
		LedgerRepo:        ledgerRepo,
		InboxRepo:         inboxRepo,
		OutboxRepo:        outboxRepo,
		CreateWalletUC:    createWalletUC,
		ProcessWagerUC:    processWagerUC,
		GetWalletUC:       getWalletUC,
		ListLedgerUC:      listLedgerUC,
		ReconcileWalletUC: reconcileWalletUC,
	}
}

func runMigrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	schemas := []string{
		`CREATE TABLE IF NOT EXISTS wallets (
			id VARCHAR(64) PRIMARY KEY,
			player_id VARCHAR(64) NOT NULL,
			currency VARCHAR(3) NOT NULL,
			balance NUMERIC(15,2) NOT NULL CHECK (balance >= 0),
			version INT NOT NULL DEFAULT 1,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (player_id, currency)
		);`,
		`CREATE TABLE IF NOT EXISTS wager_transactions (
			id VARCHAR(64) PRIMARY KEY,
			external_id VARCHAR(255) NOT NULL,
			provider_id VARCHAR(255) NOT NULL,
			idempotency_key VARCHAR(255) NOT NULL,
			payload_hash VARCHAR(255) NOT NULL,
			wallet_id VARCHAR(64) NOT NULL,
			player_id VARCHAR(64) NOT NULL,
			round_id VARCHAR(255),
			game_id VARCHAR(255),
			kind VARCHAR(50) NOT NULL,
			amount NUMERIC(15,2) NOT NULL,
			currency VARCHAR(3) NOT NULL,
			reference_external_id VARCHAR(255),
			status VARCHAR(50) NOT NULL,
			failure_code VARCHAR(50),
			balance_after NUMERIC(15,2),
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (provider_id, external_id),
			UNIQUE (provider_id, idempotency_key)
		);`,
		`CREATE TABLE IF NOT EXISTS wallet_ledger_entries (
			id VARCHAR(64) PRIMARY KEY,
			wallet_id VARCHAR(64) NOT NULL,
			transaction_id VARCHAR(64) NOT NULL,
			direction VARCHAR(10) NOT NULL,
			amount NUMERIC(15,2) NOT NULL,
			balance_before NUMERIC(15,2) NOT NULL,
			balance_after NUMERIC(15,2) NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (wallet_id, transaction_id)
		);`,
		`CREATE OR REPLACE FUNCTION prevent_ledger_mutation()
		RETURNS TRIGGER AS $$
		BEGIN
			RAISE EXCEPTION 'wallet_ledger_entries is append-only: updates and deletes are forbidden';
		END;
		$$ LANGUAGE plpgsql;`,
		`DROP TRIGGER IF EXISTS trg_protect_ledger ON wallet_ledger_entries;
		CREATE TRIGGER trg_protect_ledger
		BEFORE UPDATE OR DELETE ON wallet_ledger_entries
		FOR EACH ROW EXECUTE FUNCTION prevent_ledger_mutation();`,
		`CREATE TABLE IF NOT EXISTS inbox (
			id SERIAL PRIMARY KEY,
			consumer_name VARCHAR(255) NOT NULL,
			message_id VARCHAR(255) NOT NULL,
			payload_hash VARCHAR(255),
			status VARCHAR(50) NOT NULL DEFAULT 'PROCESSED',
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (consumer_name, message_id)
		);`,
		`CREATE TABLE IF NOT EXISTS outbox (
			id SERIAL PRIMARY KEY,
			event_id VARCHAR(255) NOT NULL UNIQUE,
			event_type VARCHAR(100) NOT NULL,
			aggregate_id VARCHAR(255) NOT NULL,
			correlation_id VARCHAR(255) NOT NULL,
			causation_id VARCHAR(255),
			payload JSONB NOT NULL,
			status VARCHAR(50) NOT NULL DEFAULT 'PENDING',
			retry_count INT NOT NULL DEFAULT 0,
			next_retry_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			published_at TIMESTAMP WITH TIME ZONE,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);`,
	}

	for _, sql := range schemas {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("failed to apply migration schema: %v", err)
		}
	}
}

func truncateTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	// Disable trigger temporarily to allow truncate/cleanup between tests
	_, err := pool.Exec(ctx, `
		ALTER TABLE wallet_ledger_entries DISABLE TRIGGER trg_protect_ledger;
		TRUNCATE TABLE outbox, inbox, wallet_ledger_entries, wager_transactions, wallets CASCADE;
		ALTER TABLE wallet_ledger_entries ENABLE TRIGGER trg_protect_ledger;
	`)
	if err != nil {
		t.Fatalf("failed to truncate tables: %v", err)
	}
}
