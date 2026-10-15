package messaging

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	"backend-challenge-go-jungle-gaming/internal/infra/database"
	"backend-challenge-go-jungle-gaming/internal/infra/metrics"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type OutboxWorker struct {
	repo         database.OutboxRepository
	txManager    database.TxManager
	sqsClient    *sqs.Client
	queueURL     string
	pollInterval time.Duration
	batchSize    int
	maxRetries   int
	logger       *slog.Logger
	wg           sync.WaitGroup
	stopCh       chan struct{}
}

func NewOutboxWorker(
	repo database.OutboxRepository,
	txManager database.TxManager,
	sqsClient *sqs.Client,
	queueURL string,
	logger *slog.Logger,
) *OutboxWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &OutboxWorker{
		repo:         repo,
		txManager:    txManager,
		sqsClient:    sqsClient,
		queueURL:     queueURL,
		pollInterval: 1 * time.Second,
		batchSize:    50,
		maxRetries:   5,
		logger:       logger,
		stopCh:       make(chan struct{}),
	}
}

func (w *OutboxWorker) Start(ctx context.Context) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.logger.Info("outbox worker started")

		ticker := time.NewTicker(w.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				w.logger.Info("outbox worker stopping due to context cancellation")
				return
			case <-w.stopCh:
				w.logger.Info("outbox worker stopping due to stop channel")
				return
			case <-ticker.C:
				w.processBatch(ctx)
			}
		}
	}()
}

func (w *OutboxWorker) Stop() {
	close(w.stopCh)
	w.wg.Wait()
	w.logger.Info("outbox worker gracefully stopped")
}

func (w *OutboxWorker) processBatch(ctx context.Context) {
	batchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()

	if w.txManager == nil {
		w.processBatchDirect(batchCtx)
		return
	}

	_ = w.txManager.WithinTransaction(batchCtx, func(txCtx context.Context) error {
		records, err := w.repo.FetchPending(txCtx, w.batchSize)
		if err != nil {
			w.logger.Error("failed to fetch pending outbox records", slog.String("error", err.Error()))
			return err
		}

		for _, rec := range records {
			if txCtx.Err() != nil {
				return txCtx.Err()
			}

			if w.sqsClient != nil && w.queueURL != "" {
				_, err := w.sqsClient.SendMessage(txCtx, &sqs.SendMessageInput{
					QueueUrl:               aws.String(w.queueURL),
					MessageBody:            aws.String(string(rec.Payload)),
					MessageGroupId:         aws.String(rec.AggregateID),
					MessageDeduplicationId: aws.String(rec.EventID),
				})
				if err != nil {
					w.logger.Error("failed to publish outbox record to sqs",
						slog.Int64("id", rec.ID),
						slog.String("event_id", rec.EventID),
						slog.String("error", err.Error()),
					)

					if rec.RetryCount >= w.maxRetries {
						_ = w.repo.MarkFailed(txCtx, rec.ID)
						metrics.IncStatus("FAILED")
					} else {
						backoff := time.Duration(math.Pow(2, float64(rec.RetryCount))) * time.Second
						_ = w.repo.ScheduleRetry(txCtx, rec.ID, time.Now().Add(backoff))
						metrics.IncRetries()
					}
					continue
				}
			}

			if err := w.repo.MarkPublished(txCtx, rec.ID, time.Now()); err != nil {
				w.logger.Error("failed to mark outbox record as published",
					slog.Int64("id", rec.ID),
					slog.String("error", err.Error()),
				)
			}
		}
		return nil
	})
}

func (w *OutboxWorker) processBatchDirect(ctx context.Context) {
	records, err := w.repo.FetchPending(ctx, w.batchSize)
	if err != nil {
		w.logger.Error("failed to fetch pending outbox records", slog.String("error", err.Error()))
		return
	}

	for _, rec := range records {
		if ctx.Err() != nil {
			return
		}

		if w.sqsClient != nil && w.queueURL != "" {
			_, err := w.sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
				QueueUrl:               aws.String(w.queueURL),
				MessageBody:            aws.String(string(rec.Payload)),
				MessageGroupId:         aws.String(rec.AggregateID),
				MessageDeduplicationId: aws.String(rec.EventID),
			})
			if err != nil {
				w.logger.Error("failed to publish outbox record to sqs",
					slog.Int64("id", rec.ID),
					slog.String("event_id", rec.EventID),
					slog.String("error", err.Error()),
				)

				if rec.RetryCount >= w.maxRetries {
					_ = w.repo.MarkFailed(ctx, rec.ID)
					metrics.IncStatus("FAILED")
				} else {
					backoff := time.Duration(math.Pow(2, float64(rec.RetryCount))) * time.Second
					_ = w.repo.ScheduleRetry(ctx, rec.ID, time.Now().Add(backoff))
					metrics.IncRetries()
				}
				continue
			}
		}

		if err := w.repo.MarkPublished(ctx, rec.ID, time.Now()); err != nil {
			w.logger.Error("failed to mark outbox record as published",
				slog.Int64("id", rec.ID),
				slog.String("error", err.Error()),
			)
		}
	}
}
