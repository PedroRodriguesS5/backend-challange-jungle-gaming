package di

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	netHttp "net/http"
	"time"

	"backend-challenge-go-jungle-gaming/internal/infra/config"
	infraHttp "backend-challenge-go-jungle-gaming/internal/infra/http"
	"backend-challenge-go-jungle-gaming/internal/infra/messaging"
	"backend-challenge-go-jungle-gaming/internal/usecase"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"
)

var HTTPModule = fx.Module("http",
	fx.Provide(
		provideAuthMiddleware,
		provideHandlers,
		provideRouter,
	),
	fx.Invoke(startHTTPServer),
)

func provideAuthMiddleware(cfg *config.Config) *infraHttp.AuthMiddleware {
	return infraHttp.NewAuthMiddleware(cfg.KeycloakIssuerURL)
}

func provideHandlers(
	pool *pgxpool.Pool,
	cfg *config.Config,
	createWalletUC *usecase.CreateWalletUseCase,
	getWalletUC *usecase.GetWalletUseCase,
	listLedgerUC *usecase.ListLedgerUseCase,
	processWagerUC *usecase.ProcessWagerUseCase,
	getTransactionUC *usecase.GetTransactionUseCase,
	reconcileWalletUC *usecase.ReconcileWalletUseCase,
) (*infraHttp.Handlers, error) {
	sqsClient, _ := messaging.NewSQSClient(context.Background(), messaging.SQSConfig{
		EndpointURL: cfg.AWSEndpointURL,
		Region:      cfg.AWSRegion,
		AccessKey:   cfg.AWSAccessKeyID,
		SecretKey:   cfg.AWSSecretAccessKey,
		QueueURL:    cfg.SQSQueueURL,
	})
	return infraHttp.NewHandlers(
		pool,
		sqsClient,
		cfg.SQSQueueURL,
		createWalletUC,
		getWalletUC,
		listLedgerUC,
		processWagerUC,
		getTransactionUC,
		reconcileWalletUC,
	), nil
}

func provideRouter(handlers *infraHttp.Handlers, authMiddleware *infraHttp.AuthMiddleware) netHttp.Handler {
	return infraHttp.NewRouter(handlers, authMiddleware)
}

func startHTTPServer(lc fx.Lifecycle, handler netHttp.Handler, cfg *config.Config, logger *slog.Logger) {
	server := &netHttp.Server{
		Addr:         fmt.Sprintf(":%s", cfg.HTTPPort),
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			logger.Info("starting http server", slog.String("addr", server.Addr))
			go func() {
				if err := server.ListenAndServe(); err != nil && !errors.Is(err, netHttp.ErrServerClosed) {
					logger.Error("http server failed", slog.String("error", err.Error()))
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			logger.Info("shutting down http server gracefully")
			shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			return server.Shutdown(shutdownCtx)
		},
	})
}
