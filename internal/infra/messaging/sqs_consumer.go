package messaging

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"backend-challenge-go-jungle-gaming/internal/domain"
	"backend-challenge-go-jungle-gaming/internal/infra/database"
	"backend-challenge-go-jungle-gaming/internal/infra/metrics"
	"backend-challenge-go-jungle-gaming/internal/usecase"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/uuid"
)

type SQSMessageEnvelope struct {
	MessageID  string          `json:"messageId"`
	Type       string          `json:"type"`
	OccurredAt string          `json:"occurredAt"`
	Data       json.RawMessage `json:"data"`
}

type SQSMessageData struct {
	ProviderID                     string           `json:"providerId"`
	ExternalTransactionID          string           `json:"externalTransactionId"`
	IdempotencyKey                 string           `json:"idempotencyKey"`
	PlayerID                       string           `json:"playerId"`
	WalletID                       string           `json:"walletId"`
	RoundID                        string           `json:"roundId"`
	GameID                         string           `json:"gameId"`
	Kind                           domain.WagerKind `json:"kind"`
	Money                          domain.Money     `json:"money"`
	ReferenceExternalTransactionID string           `json:"referenceExternalTransactionId,omitempty"`
}

type SQSConsumerWorker struct {
	sqsClient      *sqs.Client
	queueURL       string
	dlqURL         string
	consumerName   string
	txManager      database.TxManager
	inboxRepo      database.InboxRepository
	processWagerUC *usecase.ProcessWagerUseCase
	logger         *slog.Logger
	wg             sync.WaitGroup
	stopCh         chan struct{}
}

func NewSQSConsumerWorker(
	sqsClient *sqs.Client,
	queueURL string,
	dlqURL string,
	consumerName string,
	txManager database.TxManager,
	inboxRepo database.InboxRepository,
	processWagerUC *usecase.ProcessWagerUseCase,
	logger *slog.Logger,
) *SQSConsumerWorker {
	if logger == nil {
		logger = slog.Default()
	}
	if consumerName == "" {
		consumerName = "sqs-wager-consumer"
	}
	return &SQSConsumerWorker{
		sqsClient:      sqsClient,
		queueURL:       queueURL,
		dlqURL:         dlqURL,
		consumerName:   consumerName,
		txManager:      txManager,
		inboxRepo:      inboxRepo,
		processWagerUC: processWagerUC,
		logger:         logger,
		stopCh:         make(chan struct{}),
	}
}

func (w *SQSConsumerWorker) Start(ctx context.Context) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.logger.Info("sqs consumer worker started polling", slog.String("queue", w.queueURL))

		for {
			select {
			case <-ctx.Done():
				w.logger.Info("sqs consumer stopping due to context cancellation")
				return
			case <-w.stopCh:
				w.logger.Info("sqs consumer stopping due to stop signal")
				return
			default:
				w.pollAndProcess(ctx)
			}
		}
	}()
}

func (w *SQSConsumerWorker) Stop() {
	close(w.stopCh)
	w.wg.Wait()
	w.logger.Info("sqs consumer worker gracefully stopped")
}

func (w *SQSConsumerWorker) pollAndProcess(ctx context.Context) {
	if w.sqsClient == nil || w.queueURL == "" {
		time.Sleep(1 * time.Second)
		return
	}

	output, err := w.sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(w.queueURL),
		MaxNumberOfMessages: 10,
		WaitTimeSeconds:     5,
		VisibilityTimeout:   30,
	})
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return
		}
		w.logger.Error("failed to receive messages from sqs", slog.String("error", err.Error()))
		time.Sleep(1 * time.Second)
		return
	}

	for i, msg := range output.Messages {
		select {
		case <-w.stopCh:
			w.releaseMessages(output.Messages[i:])
			return
		case <-ctx.Done():
			w.releaseMessages(output.Messages[i:])
			return
		default:
			w.handleMessage(ctx, msg)
		}
	}
}

func (w *SQSConsumerWorker) handleMessage(ctx context.Context, msg types.Message) {
	procCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	if msg.Body == nil {
		w.deleteMessage(procCtx, msg)
		return
	}

	var env SQSMessageEnvelope
	if err := json.Unmarshal([]byte(*msg.Body), &env); err != nil {
		w.logger.Error("malformed sqs envelope, moving to dlq", slog.String("body", *msg.Body), slog.String("error", err.Error()))
		w.sendToDLQ(procCtx, *msg.Body, "malformed-envelope", aws.ToString(msg.MessageId))
		w.deleteMessage(procCtx, msg)
		return
	}

	var data SQSMessageData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		w.logger.Error("malformed sqs message data, moving to dlq", slog.String("error", err.Error()))
		w.sendToDLQ(procCtx, *msg.Body, "malformed-data", env.MessageID)
		w.deleteMessage(procCtx, msg)
		return
	}

	bodyHash := sha256.Sum256([]byte(*msg.Body))
	hashStr := hex.EncodeToString(bodyHash[:])

	var hashMismatchErr bool
	var out *usecase.ProcessWagerOutput

	err := w.txManager.WithinTransaction(procCtx, func(txCtx context.Context) error {
		err := w.inboxRepo.Record(txCtx, w.consumerName, env.MessageID, hashStr, time.Now())
		if err != nil {
			if errors.Is(err, database.ErrDuplicateInboxMessage) {
				existingHash, hashErr := w.inboxRepo.GetPayloadHash(txCtx, w.consumerName, env.MessageID)
				if hashErr == nil && existingHash != hashStr {
					w.logger.Error("inbox message ID reused with mismatched payload hash",
						slog.String("messageId", env.MessageID),
						slog.String("existingHash", existingHash),
						slog.String("newHash", hashStr),
					)
					hashMismatchErr = true
					return errors.New("inbox payload hash mismatch on redelivery")
				}
				metrics.IncDuplicates()
				w.logger.Info("sqs message already recorded in inbox with matching hash, deduplicated safely", slog.String("messageId", env.MessageID))
				return nil
			}
			return err
		}

		out, err = w.processWagerUC.Execute(txCtx, usecase.ProcessWagerInput{
			ProviderID:                     data.ProviderID,
			ExternalTransactionID:          data.ExternalTransactionID,
			IdempotencyKey:                 data.IdempotencyKey,
			PlayerID:                       data.PlayerID,
			WalletID:                       data.WalletID,
			RoundID:                        data.RoundID,
			GameID:                         data.GameID,
			Kind:                           data.Kind,
			Money:                          data.Money,
			ReferenceExternalTransactionID: data.ReferenceExternalTransactionID,
		})
		return err
	})

	if err != nil {
		if hashMismatchErr {
			// Permanent poison/tampered message: move to DLQ and remove from main queue
			w.sendToDLQ(procCtx, *msg.Body, "hash-mismatch", env.MessageID)
			w.deleteMessage(procCtx, msg)
			return
		}

		w.logger.Error("failed to process message in transaction, keeping in queue for retry",
			slog.String("messageId", env.MessageID),
			slog.String("error", err.Error()),
		)
		// Do not delete message; let SQS redrive policy redeliver or move to DLQ
		return
	}

	if out != nil {
		w.logger.Info("sqs wager transaction completed",
			slog.String("messageId", env.MessageID),
			slog.String("transactionId", out.TransactionID),
			slog.String("walletId", data.WalletID),
			slog.String("providerId", data.ProviderID),
			slog.String("status", string(out.Status)),
		)
	}

	w.deleteMessage(procCtx, msg)
}

func (w *SQSConsumerWorker) releaseMessages(msgs []types.Message) {
	if w.sqsClient == nil || len(msgs) == 0 {
		return
	}
	relCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, msg := range msgs {
		if msg.ReceiptHandle != nil {
			_, err := w.sqsClient.ChangeMessageVisibility(relCtx, &sqs.ChangeMessageVisibilityInput{
				QueueUrl:          aws.String(w.queueURL),
				ReceiptHandle:     msg.ReceiptHandle,
				VisibilityTimeout: 0,
			})
			if err != nil {
				w.logger.Warn("failed to release message visibility", slog.String("error", err.Error()))
			}
		}
	}
}

func (w *SQSConsumerWorker) sendToDLQ(ctx context.Context, body string, groupID string, dedupID string) {
	metrics.IncDLQ()
	if w.sqsClient == nil || w.dlqURL == "" {
		return
	}
	if dedupID == "" {
		dedupID = uuid.NewString()
	}
	_, err := w.sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               aws.String(w.dlqURL),
		MessageBody:            aws.String(body),
		MessageGroupId:         aws.String(groupID),
		MessageDeduplicationId: aws.String(dedupID),
	})
	if err != nil {
		w.logger.Error("failed to send message to dlq", slog.String("error", err.Error()))
	}
}

func (w *SQSConsumerWorker) deleteMessage(ctx context.Context, msg types.Message) {
	if w.sqsClient == nil || msg.ReceiptHandle == nil {
		return
	}
	_, err := w.sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(w.queueURL),
		ReceiptHandle: msg.ReceiptHandle,
	})
	if err != nil {
		w.logger.Error("failed to delete message from sqs", slog.String("error", err.Error()))
	}
}
