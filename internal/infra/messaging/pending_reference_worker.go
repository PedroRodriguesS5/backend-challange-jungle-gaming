package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"backend-challenge-go-jungle-gaming/internal/domain"
	"backend-challenge-go-jungle-gaming/internal/infra/database"
	"backend-challenge-go-jungle-gaming/internal/infra/metrics"
	"github.com/google/uuid"
)

type PendingReferenceWorker struct {
	txManager    database.TxManager
	wagerRepo    database.WagerRepository
	walletRepo   database.WalletRepository
	ledgerRepo   database.LedgerRepository
	outboxRepo   database.OutboxRepository
	pollInterval time.Duration
	ttl          time.Duration
	batchSize    int
	logger       *slog.Logger
	wg           sync.WaitGroup
	stopCh       chan struct{}
}

func NewPendingReferenceWorker(
	txManager database.TxManager,
	wagerRepo database.WagerRepository,
	walletRepo database.WalletRepository,
	ledgerRepo database.LedgerRepository,
	outboxRepo database.OutboxRepository,
	logger *slog.Logger,
) *PendingReferenceWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &PendingReferenceWorker{
		txManager:    txManager,
		wagerRepo:    wagerRepo,
		walletRepo:   walletRepo,
		ledgerRepo:   ledgerRepo,
		outboxRepo:   outboxRepo,
		pollInterval: 2 * time.Second,
		ttl:          5 * time.Minute,
		batchSize:    20,
		logger:       logger,
		stopCh:       make(chan struct{}),
	}
}

func (w *PendingReferenceWorker) Start(ctx context.Context) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.logger.Info("pending reference retry worker started")

		ticker := time.NewTicker(w.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				w.logger.Info("pending reference worker stopping due to context cancellation")
				return
			case <-w.stopCh:
				w.logger.Info("pending reference worker stopping due to stop channel")
				return
			case <-ticker.C:
				w.processPending(ctx)
			}
		}
	}()
}

func (w *PendingReferenceWorker) Stop() {
	close(w.stopCh)
	w.wg.Wait()
	w.logger.Info("pending reference retry worker gracefully stopped")
}

func (w *PendingReferenceWorker) processPending(ctx context.Context) {
	pending, err := w.wagerRepo.FindPendingReferences(ctx, w.batchSize)
	if err != nil {
		w.logger.Error("failed to find pending references", slog.String("error", err.Error()))
		return
	}

	for _, tx := range pending {
		if ctx.Err() != nil {
			return
		}
		w.retryOne(ctx, tx)
	}
}

func (w *PendingReferenceWorker) retryOne(ctx context.Context, tx *domain.WagerTransaction) {
	metrics.IncRetries()
	retryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	now := time.Now().UTC()

	// Check if TTL has expired
	if now.Sub(tx.CreatedAt()) > w.ttl {
		_ = w.txManager.WithinTransaction(retryCtx, func(txCtx context.Context) error {
			if wallet, err := w.walletRepo.FindByID(txCtx, tx.WalletID()); err == nil && wallet != nil {
				tx.SetBalanceAfter(wallet.Balance())
			}
			if err := tx.TransitionToRejected(domain.FailureCodeReferenceNotFound, now); err != nil {
				return err
			}
			if err := w.wagerRepo.Update(txCtx, tx); err != nil {
				return err
			}

			rejectedEvent, err := domain.NewEventEnvelope(
				domain.EventTypeWagerTransactionRejected,
				tx.WalletID(),
				uuid.NewString(),
				"",
				now,
				1,
				domain.WagerTransactionRejectedData{
					TransactionID:       tx.ID(),
					ExternalID:          tx.ExternalID(),
					ProviderID:          tx.ProviderID(),
					WalletID:            tx.WalletID(),
					PlayerID:            tx.PlayerID(),
					Kind:                string(tx.Kind()),
					Amount:              tx.Amount(),
					ReferenceExternalID: tx.ReferenceExternalID(),
					FailureCode:         domain.FailureCodeReferenceNotFound,
				},
			)
			if err != nil {
				return err
			}

			envelopeBytes, err := json.Marshal(rejectedEvent)
			if err != nil {
				return err
			}

			return w.outboxRepo.Save(txCtx, &database.OutboxRecord{
				EventID:       rejectedEvent.EventID,
				EventType:     rejectedEvent.EventType,
				AggregateID:   rejectedEvent.AggregateID,
				CorrelationID: rejectedEvent.CorrelationID,
				Payload:       envelopeBytes,
				CreatedAt:     now,
			})
		})
		return
	}

	_ = w.txManager.WithinTransaction(retryCtx, func(txCtx context.Context) error {
		refTx, err := w.wagerRepo.FindByProviderAndExternalID(txCtx, tx.ProviderID(), tx.ReferenceExternalID())
		if err != nil || refTx == nil || refTx.Status() != domain.StatusProcessed {
			return nil // reference still unavailable or not processed yet
		}

		wallet, err := w.walletRepo.FindByIDForUpdate(txCtx, tx.WalletID())
		if err != nil {
			return err
		}

		if err := tx.ValidateReference(refTx); err != nil {
			return w.rejectPending(txCtx, tx, wallet, domain.FailureCodeReferenceMismatch, now)
		}

		alreadyReversed, err := w.wagerRepo.HasSuccessfulReversal(txCtx, tx.ProviderID(), tx.ReferenceExternalID())
		if err != nil {
			return err
		}
		if alreadyReversed {
			return w.rejectPending(txCtx, tx, wallet, domain.FailureCodeReferenceAlreadyReversed, now)
		}

		ledgerEntryID := uuid.NewString()
		var (
			ledgerEntry domain.WalletLedgerEntry
			reversalErr error
		)

		if tx.Kind() == domain.KindRefund {
			if refTx.Kind() != domain.KindBet {
				return w.rejectPending(txCtx, tx, wallet, domain.FailureCodeReferenceMismatch, now)
			}
			ledgerEntry, reversalErr = wallet.Credit(tx.ID(), ledgerEntryID, tx.Amount(), now)
		} else if tx.Kind() == domain.KindRollback {
			switch refTx.Kind() {
			case domain.KindBet:
				ledgerEntry, reversalErr = wallet.Credit(tx.ID(), ledgerEntryID, tx.Amount(), now)
			case domain.KindWin, domain.KindRefund:
				ledgerEntry, reversalErr = wallet.Debit(tx.ID(), ledgerEntryID, tx.Amount(), now)
				if errors.Is(reversalErr, domain.ErrInsufficientFunds) {
					return w.rejectPending(txCtx, tx, wallet, domain.FailureCodeReversalExceedsBalance, now)
				}
			default:
				return w.rejectPending(txCtx, tx, wallet, domain.FailureCodeReferenceMismatch, now)
			}
		}

		if reversalErr != nil {
			return reversalErr
		}

		tx.SetBalanceAfter(wallet.Balance())
		if err := tx.TransitionToProcessed(now); err != nil {
			return err
		}
		if err := w.wagerRepo.Update(txCtx, tx); err != nil {
			return err
		}
		if err := w.walletRepo.Update(txCtx, wallet); err != nil {
			return err
		}
		if err := w.ledgerRepo.Create(txCtx, ledgerEntry); err != nil {
			return err
		}

		processedEvent, err := domain.NewEventEnvelope(
			domain.EventTypeWagerTransactionProcessed,
			tx.WalletID(),
			uuid.NewString(),
			"",
			now,
			1,
			domain.WagerTransactionProcessedData{
				TransactionID:       tx.ID(),
				ExternalID:          tx.ExternalID(),
				ProviderID:          tx.ProviderID(),
				WalletID:            tx.WalletID(),
				PlayerID:            tx.PlayerID(),
				RoundID:             tx.RoundID(),
				GameID:              tx.GameID(),
				Kind:                string(tx.Kind()),
				Amount:              tx.Amount(),
				ReferenceExternalID: tx.ReferenceExternalID(),
				Status:              string(tx.Status()),
			},
		)
		if err != nil {
			return err
		}

		processedBytes, err := json.Marshal(processedEvent)
		if err != nil {
			return err
		}

		if err := w.outboxRepo.Save(txCtx, &database.OutboxRecord{
			EventID:       processedEvent.EventID,
			EventType:     processedEvent.EventType,
			AggregateID:   processedEvent.AggregateID,
			CorrelationID: processedEvent.CorrelationID,
			Payload:       processedBytes,
			CreatedAt:     now,
		}); err != nil {
			return err
		}

		balanceEvent, err := domain.NewEventEnvelope(
			domain.EventTypeWalletBalanceChanged,
			tx.WalletID(),
			uuid.NewString(),
			tx.ID(),
			now,
			1,
			domain.WalletBalanceChangedData{
				WalletID:      tx.WalletID(),
				TransactionID: tx.ID(),
				Direction:     string(ledgerEntry.Direction()),
				Money:         ledgerEntry.Amount(),
				BalanceBefore: ledgerEntry.BalanceBefore(),
				BalanceAfter:  ledgerEntry.BalanceAfter(),
				WalletVersion: wallet.Version(),
			},
		)
		if err != nil {
			return err
		}

		balanceBytes, err := json.Marshal(balanceEvent)
		if err != nil {
			return err
		}

		return w.outboxRepo.Save(txCtx, &database.OutboxRecord{
			EventID:       balanceEvent.EventID,
			EventType:     balanceEvent.EventType,
			AggregateID:   balanceEvent.AggregateID,
			CorrelationID: balanceEvent.CorrelationID,
			CausationID:   tx.ID(),
			Payload:       balanceBytes,
			CreatedAt:     now,
		})
	})
}

func (w *PendingReferenceWorker) rejectPending(ctx context.Context, tx *domain.WagerTransaction, wallet *domain.Wallet, failureCode string, now time.Time) error {
	if wallet != nil {
		tx.SetBalanceAfter(wallet.Balance())
	}
	if err := tx.TransitionToRejected(failureCode, now); err != nil {
		return err
	}
	if err := w.wagerRepo.Update(ctx, tx); err != nil {
		return err
	}

	rejectedEvent, err := domain.NewEventEnvelope(
		domain.EventTypeWagerTransactionRejected,
		tx.WalletID(),
		uuid.NewString(),
		"",
		now,
		1,
		domain.WagerTransactionRejectedData{
			TransactionID:       tx.ID(),
			ExternalID:          tx.ExternalID(),
			ProviderID:          tx.ProviderID(),
			WalletID:            tx.WalletID(),
			PlayerID:            tx.PlayerID(),
			Kind:                string(tx.Kind()),
			Amount:              tx.Amount(),
			ReferenceExternalID: tx.ReferenceExternalID(),
			FailureCode:         failureCode,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create rejected event: %w", err)
	}

	rejectedBytes, err := json.Marshal(rejectedEvent)
	if err != nil {
		return err
	}

	return w.outboxRepo.Save(ctx, &database.OutboxRecord{
		EventID:       rejectedEvent.EventID,
		EventType:     rejectedEvent.EventType,
		AggregateID:   rejectedEvent.AggregateID,
		CorrelationID: rejectedEvent.CorrelationID,
		Payload:       rejectedBytes,
		CreatedAt:     now,
	})
}
