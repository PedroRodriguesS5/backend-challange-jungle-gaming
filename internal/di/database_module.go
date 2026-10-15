package di

import (
	"context"
	"log/slog"

	"backend-challenge-go-jungle-gaming/internal/infra/config"
	"backend-challenge-go-jungle-gaming/internal/infra/database"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"
)

var DatabaseModule = fx.Module("database",
	fx.Provide(
		providePgxPool,
		provideTxManager,
		provideWalletRepository,
		provideLedgerRepository,
		provideWagerRepository,
		provideInboxRepository,
		provideOutboxRepository,
	),
)

func providePgxPool(lc fx.Lifecycle, cfg *config.Config, logger *slog.Logger) (*pgxpool.Pool, error) {
	if logger == nil {
		logger = slog.Default()
	}

	ctx := context.Background()
	pool, err := database.NewPool(ctx, database.Config{
		DatabaseURL: cfg.DatabaseURL,
	})
	if err != nil {
		return nil, err
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			logger.Info("database pool initialized and connected")
			return pool.Ping(ctx)
		},
		OnStop: func(ctx context.Context) error {
			logger.Info("closing database connection pool after workers/server have finished")
			pool.Close()
			return nil
		},
	})

	return pool, nil
}

func provideTxManager(pool *pgxpool.Pool) database.TxManager {
	return database.NewPgxTxManager(pool)
}

func provideWalletRepository(pool *pgxpool.Pool) database.WalletRepository {
	return database.NewWalletRepository(pool)
}

func provideLedgerRepository(pool *pgxpool.Pool) database.LedgerRepository {
	return database.NewLedgerRepository(pool)
}

func provideWagerRepository(pool *pgxpool.Pool) database.WagerRepository {
	return database.NewWagerRepository(pool)
}

func provideInboxRepository(pool *pgxpool.Pool) database.InboxRepository {
	return database.NewInboxRepository(pool)
}

func provideOutboxRepository(pool *pgxpool.Pool) database.OutboxRepository {
	return database.NewOutboxRepository(pool)
}
