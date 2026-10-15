package di

import (
	"context"
	"log/slog"

	"backend-challenge-go-jungle-gaming/internal/infra/config"
	"backend-challenge-go-jungle-gaming/internal/infra/database"
	"backend-challenge-go-jungle-gaming/internal/infra/messaging"
	"backend-challenge-go-jungle-gaming/internal/usecase"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"go.uber.org/fx"
)

var MessagingModule = fx.Module("messaging",
	fx.Provide(
		provideSQSClient,
		provideOutboxWorker,
		provideSQSConsumerWorker,
		providePendingReferenceWorker,
	),
	fx.Invoke(startMessagingWorkers),
)

func provideSQSClient(cfg *config.Config) (*sqs.Client, error) {
	return messaging.NewSQSClient(context.Background(), messaging.SQSConfig{
		EndpointURL: cfg.AWSEndpointURL,
		Region:      cfg.AWSRegion,
		AccessKey:   cfg.AWSAccessKeyID,
		SecretKey:   cfg.AWSSecretAccessKey,
		QueueURL:    cfg.SQSQueueURL,
		DLQURL:      cfg.SQSDLQURL,
	})
}

func provideOutboxWorker(
	repo database.OutboxRepository,
	txManager database.TxManager,
	sqsClient *sqs.Client,
	cfg *config.Config,
	logger *slog.Logger,
) *messaging.OutboxWorker {
	return messaging.NewOutboxWorker(repo, txManager, sqsClient, cfg.SQSEventsQueueURL, logger)
}

func provideSQSConsumerWorker(
	sqsClient *sqs.Client,
	cfg *config.Config,
	txManager database.TxManager,
	inboxRepo database.InboxRepository,
	processWagerUC *usecase.ProcessWagerUseCase,
	logger *slog.Logger,
) *messaging.SQSConsumerWorker {
	return messaging.NewSQSConsumerWorker(
		sqsClient,
		cfg.SQSQueueURL,
		cfg.SQSDLQURL,
		"wager-service-consumer",
		txManager,
		inboxRepo,
		processWagerUC,
		logger,
	)
}

func providePendingReferenceWorker(
	txManager database.TxManager,
	wagerRepo database.WagerRepository,
	walletRepo database.WalletRepository,
	ledgerRepo database.LedgerRepository,
	outboxRepo database.OutboxRepository,
	logger *slog.Logger,
) *messaging.PendingReferenceWorker {
	return messaging.NewPendingReferenceWorker(
		txManager,
		wagerRepo,
		walletRepo,
		ledgerRepo,
		outboxRepo,
		logger,
	)
}

func startMessagingWorkers(
	lc fx.Lifecycle,
	outboxWorker *messaging.OutboxWorker,
	consumerWorker *messaging.SQSConsumerWorker,
	pendingRefWorker *messaging.PendingReferenceWorker,
	logger *slog.Logger,
) {
	workerCtx, cancel := context.WithCancel(context.Background())

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			logger.Info("starting all background workers in isolated goroutines")
			outboxWorker.Start(workerCtx)
			consumerWorker.Start(workerCtx)
			pendingRefWorker.Start(workerCtx)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			logger.Info("gracefully stopping workers: halting polling and awaiting in-flight tasks")
			cancel()
			consumerWorker.Stop()
			pendingRefWorker.Stop()
			outboxWorker.Stop()
			logger.Info("all workers stopped successfully before database shutdown")
			return nil
		},
	})
}
