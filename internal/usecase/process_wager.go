package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"backend-challenge-go-jungle-gaming/internal/domain"
	"backend-challenge-go-jungle-gaming/internal/infra/database"
	"backend-challenge-go-jungle-gaming/internal/infra/metrics"
	"github.com/google/uuid"
)

type ProcessWagerInput struct {
	ProviderID                     string
	ExternalTransactionID          string
	IdempotencyKey                 string
	PlayerID                       string
	WalletID                       string
	RoundID                        string
	GameID                         string
	Kind                           domain.WagerKind
	Money                          domain.Money
	ReferenceExternalTransactionID string
}

type ProcessWagerOutput struct {
	TransactionID    string             `json:"transactionId"`
	Status           domain.WagerStatus `json:"status"`
	Balance          domain.Money       `json:"balance"`
	IdempotentReplay bool               `json:"idempotentReplay"`
	FailureCode      string             `json:"failureCode,omitempty"`
}

type ProcessWagerUseCase struct {
	txManager  database.TxManager
	walletRepo database.WalletRepository
	wagerRepo  database.WagerRepository
	ledgerRepo database.LedgerRepository
	outboxRepo database.OutboxRepository
}

func NewProcessWagerUseCase(
	txManager database.TxManager,
	walletRepo database.WalletRepository,
	wagerRepo database.WagerRepository,
	ledgerRepo database.LedgerRepository,
	outboxRepo database.OutboxRepository,
) *ProcessWagerUseCase {
	return &ProcessWagerUseCase{
		txManager:  txManager,
		walletRepo: walletRepo,
		wagerRepo:  wagerRepo,
		ledgerRepo: ledgerRepo,
		outboxRepo: outboxRepo,
	}
}

func (uc *ProcessWagerUseCase) Execute(ctx context.Context, input ProcessWagerInput) (*ProcessWagerOutput, error) {
	if input.Kind == domain.KindOpening {
		return nil, domain.NewDomainError(
			domain.FailureCodeInternalOnly,
			"opening kind is reserved for internal operations",
			domain.ErrInternalTransactionForbidden,
		)
	}

	payloadHash, err := ComputePayloadHash(
		input.Money,
		input.ExternalTransactionID,
		input.GameID,
		input.Kind,
		input.PlayerID,
		input.ProviderID,
		input.ReferenceExternalTransactionID,
		input.RoundID,
		input.WalletID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to compute payload hash: %w", err)
	}

	var output ProcessWagerOutput
	now := time.Now().UTC()

	err = uc.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		existing, err := uc.wagerRepo.FindByProviderAndExternalID(txCtx, input.ProviderID, input.ExternalTransactionID)
		if err == nil && existing != nil {
			return uc.handleExistingTransaction(txCtx, existing, input, payloadHash, &output)
		}

		existingByKey, err := uc.wagerRepo.FindByProviderAndIdempotencyKey(txCtx, input.ProviderID, input.IdempotencyKey)
		if err == nil && existingByKey != nil {
			return uc.handleExistingTransaction(txCtx, existingByKey, input, payloadHash, &output)
		}

		wallet, err := uc.walletRepo.FindByIDForUpdate(txCtx, input.WalletID)
		if err != nil {
			return fmt.Errorf("failed to lock wallet: %w", err)
		}

		if wallet.PlayerID() != input.PlayerID {
			return domain.NewDomainError(
				domain.FailureCodeUnauthorizedProvider,
				"wallet does not belong to specified player",
				nil,
			)
		}

		if wallet.Currency() != input.Money.Currency() {
			return domain.NewDomainError(
				domain.FailureCodeCurrencyMismatch,
				"currency mismatch between wallet and transaction",
				domain.ErrCurrencyMismatch,
			)
		}

		newTxID := uuid.NewString()
		tx, err := domain.NewWagerTransaction(
			newTxID,
			input.ExternalTransactionID,
			input.ProviderID,
			input.IdempotencyKey,
			payloadHash,
			input.WalletID,
			input.PlayerID,
			input.RoundID,
			input.GameID,
			input.Kind,
			input.Money,
			input.ReferenceExternalTransactionID,
			now,
		)
		if err != nil {
			return err
		}

		switch input.Kind {
		case domain.KindLoss:
			return uc.processLoss(txCtx, tx, wallet, now, &output)
		case domain.KindBet:
			return uc.processBet(txCtx, tx, wallet, now, &output)
		case domain.KindWin:
			return uc.processWin(txCtx, tx, wallet, now, &output)
		case domain.KindRefund:
			return uc.processRefund(txCtx, tx, wallet, now, &output)
		case domain.KindRollback:
			return uc.processRollback(txCtx, tx, wallet, now, &output)
		default:
			return domain.ErrInvalidWagerKind
		}
	})

	if err != nil {
		var domErr *domain.DomainError
		if errors.As(err, &domErr) && domErr.Code == domain.FailureCodeIdempotencyConflict {
			existing, readErr := uc.wagerRepo.FindByProviderAndExternalID(ctx, input.ProviderID, input.ExternalTransactionID)
			if readErr == nil && existing != nil {
				var replayOut ProcessWagerOutput
				if handleErr := uc.handleExistingTransaction(ctx, existing, input, payloadHash, &replayOut); handleErr == nil {
					return &replayOut, nil
				} else {
					metrics.IncConflicts()
					return nil, handleErr
				}
			}
			existingByKey, readErrKey := uc.wagerRepo.FindByProviderAndIdempotencyKey(ctx, input.ProviderID, input.IdempotencyKey)
			if readErrKey == nil && existingByKey != nil {
				var replayOut ProcessWagerOutput
				if handleErr := uc.handleExistingTransaction(ctx, existingByKey, input, payloadHash, &replayOut); handleErr == nil {
					return &replayOut, nil
				} else {
					metrics.IncConflicts()
					return nil, handleErr
				}
			}
			metrics.IncConflicts()
		}
		return nil, err
	}

	if !output.IdempotentReplay {
		metrics.IncStatus(string(output.Status))
		slog.Info("wager transaction processed",
			slog.String("transactionId", output.TransactionID),
			slog.String("walletId", input.WalletID),
			slog.String("providerId", input.ProviderID),
			slog.String("status", string(output.Status)),
		)
	}

	return &output, nil
}

func (uc *ProcessWagerUseCase) handleExistingTransaction(
	ctx context.Context,
	existing *domain.WagerTransaction,
	input ProcessWagerInput,
	payloadHash string,
	output *ProcessWagerOutput,
) error {
	if existing.IdempotencyKey() != input.IdempotencyKey ||
		existing.ExternalID() != input.ExternalTransactionID ||
		existing.PayloadHash() != payloadHash {
		metrics.IncConflicts()
		return domain.NewDomainError(
			domain.FailureCodeIdempotencyConflict,
			"idempotency key or transaction ID reused with different payload",
			domain.ErrIdempotencyConflict,
		)
	}

	var balance domain.Money
	if existing.BalanceAfter() != nil {
		balance = *existing.BalanceAfter()
	} else {
		wallet, err := uc.walletRepo.FindByID(ctx, existing.WalletID())
		if err != nil {
			return fmt.Errorf("failed to find wallet for existing transaction: %w", err)
		}
		balance = wallet.Balance()
	}

	metrics.IncDuplicates()
	slog.Info("wager transaction processed",
		slog.String("transactionId", existing.ID()),
		slog.String("walletId", existing.WalletID()),
		slog.String("providerId", existing.ProviderID()),
		slog.String("status", string(existing.Status())),
		slog.Bool("idempotentReplay", true),
	)

	*output = ProcessWagerOutput{
		TransactionID:    existing.ID(),
		Status:           existing.Status(),
		Balance:          balance,
		IdempotentReplay: true,
		FailureCode:      existing.FailureCode(),
	}
	return nil
}

func (uc *ProcessWagerUseCase) processLoss(
	ctx context.Context,
	tx *domain.WagerTransaction,
	wallet *domain.Wallet,
	now time.Time,
	output *ProcessWagerOutput,
) error {
	tx.SetBalanceAfter(wallet.Balance())
	if err := tx.TransitionToProcessed(now); err != nil {
		return err
	}
	if err := uc.wagerRepo.Create(ctx, tx); err != nil {
		return err
	}

	if err := uc.publishProcessedEvent(ctx, tx, wallet.ID(), now); err != nil {
		return err
	}

	*output = ProcessWagerOutput{
		TransactionID:    tx.ID(),
		Status:           tx.Status(),
		Balance:          wallet.Balance(),
		IdempotentReplay: false,
	}
	return nil
}

func (uc *ProcessWagerUseCase) processBet(
	ctx context.Context,
	tx *domain.WagerTransaction,
	wallet *domain.Wallet,
	now time.Time,
	output *ProcessWagerOutput,
) error {
	ledgerEntryID := uuid.NewString()
	ledgerEntry, err := wallet.Debit(tx.ID(), ledgerEntryID, tx.Amount(), now)
	if err != nil {
		if errors.Is(err, domain.ErrInsufficientFunds) {
			return uc.rejectTransaction(ctx, tx, wallet, domain.FailureCodeInsufficientFunds, now, output)
		}
		return err
	}

	tx.SetBalanceAfter(wallet.Balance())
	if err := tx.TransitionToProcessed(now); err != nil {
		return err
	}
	if err := uc.wagerRepo.Create(ctx, tx); err != nil {
		return err
	}
	if err := uc.walletRepo.Update(ctx, wallet); err != nil {
		return err
	}
	if err := uc.ledgerRepo.Create(ctx, ledgerEntry); err != nil {
		return err
	}

	if err := uc.publishProcessedEvent(ctx, tx, wallet.ID(), now); err != nil {
		return err
	}
	if err := uc.publishBalanceChangedEvent(ctx, wallet, tx.ID(), ledgerEntry, now); err != nil {
		return err
	}

	*output = ProcessWagerOutput{
		TransactionID:    tx.ID(),
		Status:           tx.Status(),
		Balance:          wallet.Balance(),
		IdempotentReplay: false,
	}
	return nil
}

func (uc *ProcessWagerUseCase) processWin(
	ctx context.Context,
	tx *domain.WagerTransaction,
	wallet *domain.Wallet,
	now time.Time,
	output *ProcessWagerOutput,
) error {
	if tx.ReferenceExternalID() != "" {
		refTx, err := uc.wagerRepo.FindByProviderAndExternalID(ctx, tx.ProviderID(), tx.ReferenceExternalID())
		if err != nil || refTx == nil {
			return uc.rejectTransaction(ctx, tx, wallet, domain.FailureCodeReferenceNotFound, now, output)
		}
		if refTx.Status() != domain.StatusProcessed || refTx.Kind() != domain.KindBet {
			return uc.rejectTransaction(ctx, tx, wallet, domain.FailureCodeReferenceMismatch, now, output)
		}
		if refTx.PlayerID() != tx.PlayerID() || refTx.WalletID() != tx.WalletID() ||
			refTx.Amount().Currency() != tx.Amount().Currency() || refTx.RoundID() != tx.RoundID() {
			return uc.rejectTransaction(ctx, tx, wallet, domain.FailureCodeReferenceMismatch, now, output)
		}
	}

	ledgerEntryID := uuid.NewString()
	ledgerEntry, err := wallet.Credit(tx.ID(), ledgerEntryID, tx.Amount(), now)
	if err != nil {
		return err
	}

	tx.SetBalanceAfter(wallet.Balance())
	if err := tx.TransitionToProcessed(now); err != nil {
		return err
	}
	if err := uc.wagerRepo.Create(ctx, tx); err != nil {
		return err
	}
	if err := uc.walletRepo.Update(ctx, wallet); err != nil {
		return err
	}
	if err := uc.ledgerRepo.Create(ctx, ledgerEntry); err != nil {
		return err
	}

	if err := uc.publishProcessedEvent(ctx, tx, wallet.ID(), now); err != nil {
		return err
	}
	if err := uc.publishBalanceChangedEvent(ctx, wallet, tx.ID(), ledgerEntry, now); err != nil {
		return err
	}

	*output = ProcessWagerOutput{
		TransactionID:    tx.ID(),
		Status:           tx.Status(),
		Balance:          wallet.Balance(),
		IdempotentReplay: false,
	}
	return nil
}

func (uc *ProcessWagerUseCase) processRefund(
	ctx context.Context,
	tx *domain.WagerTransaction,
	wallet *domain.Wallet,
	now time.Time,
	output *ProcessWagerOutput,
) error {
	refTx, err := uc.wagerRepo.FindByProviderAndExternalID(ctx, tx.ProviderID(), tx.ReferenceExternalID())
	if err != nil || refTx == nil {
		return uc.deferToPendingReference(ctx, tx, wallet, now, output)
	}

	if refTx.Status() == domain.StatusPending || refTx.Status() == domain.StatusPendingReference {
		return uc.deferToPendingReference(ctx, tx, wallet, now, output)
	}

	if err := tx.ValidateReference(refTx); err != nil {
		return uc.rejectTransaction(ctx, tx, wallet, domain.FailureCodeReferenceMismatch, now, output)
	}

	if refTx.Kind() != domain.KindBet {
		return uc.rejectTransaction(ctx, tx, wallet, domain.FailureCodeReferenceMismatch, now, output)
	}

	alreadyReversed, err := uc.wagerRepo.HasSuccessfulReversal(ctx, tx.ProviderID(), tx.ReferenceExternalID())
	if err != nil {
		return err
	}
	if alreadyReversed {
		return uc.rejectTransaction(ctx, tx, wallet, domain.FailureCodeReferenceAlreadyReversed, now, output)
	}

	ledgerEntryID := uuid.NewString()
	ledgerEntry, err := wallet.Credit(tx.ID(), ledgerEntryID, tx.Amount(), now)
	if err != nil {
		return err
	}

	tx.SetBalanceAfter(wallet.Balance())
	if err := tx.TransitionToProcessed(now); err != nil {
		return err
	}
	if err := uc.wagerRepo.Create(ctx, tx); err != nil {
		return err
	}
	if err := uc.walletRepo.Update(ctx, wallet); err != nil {
		return err
	}
	if err := uc.ledgerRepo.Create(ctx, ledgerEntry); err != nil {
		return err
	}

	if err := uc.publishProcessedEvent(ctx, tx, wallet.ID(), now); err != nil {
		return err
	}
	if err := uc.publishBalanceChangedEvent(ctx, wallet, tx.ID(), ledgerEntry, now); err != nil {
		return err
	}

	*output = ProcessWagerOutput{
		TransactionID:    tx.ID(),
		Status:           tx.Status(),
		Balance:          wallet.Balance(),
		IdempotentReplay: false,
	}
	return nil
}

func (uc *ProcessWagerUseCase) processRollback(
	ctx context.Context,
	tx *domain.WagerTransaction,
	wallet *domain.Wallet,
	now time.Time,
	output *ProcessWagerOutput,
) error {
	refTx, err := uc.wagerRepo.FindByProviderAndExternalID(ctx, tx.ProviderID(), tx.ReferenceExternalID())
	if err != nil || refTx == nil {
		return uc.deferToPendingReference(ctx, tx, wallet, now, output)
	}

	if refTx.Status() == domain.StatusPending || refTx.Status() == domain.StatusPendingReference {
		return uc.deferToPendingReference(ctx, tx, wallet, now, output)
	}

	if err := tx.ValidateReference(refTx); err != nil {
		return uc.rejectTransaction(ctx, tx, wallet, domain.FailureCodeReferenceMismatch, now, output)
	}

	alreadyReversed, err := uc.wagerRepo.HasSuccessfulReversal(ctx, tx.ProviderID(), tx.ReferenceExternalID())
	if err != nil {
		return err
	}
	if alreadyReversed {
		return uc.rejectTransaction(ctx, tx, wallet, domain.FailureCodeReferenceAlreadyReversed, now, output)
	}

	ledgerEntryID := uuid.NewString()
	var (
		ledgerEntry domain.WalletLedgerEntry
		reversalErr error
	)

	switch refTx.Kind() {
	case domain.KindBet:
		ledgerEntry, reversalErr = wallet.Credit(tx.ID(), ledgerEntryID, tx.Amount(), now)
	case domain.KindWin, domain.KindRefund:
		ledgerEntry, reversalErr = wallet.Debit(tx.ID(), ledgerEntryID, tx.Amount(), now)
		if errors.Is(reversalErr, domain.ErrInsufficientFunds) {
			return uc.rejectTransaction(ctx, tx, wallet, domain.FailureCodeReversalExceedsBalance, now, output)
		}
	default:
		return uc.rejectTransaction(ctx, tx, wallet, domain.FailureCodeReferenceMismatch, now, output)
	}

	if reversalErr != nil {
		return reversalErr
	}

	tx.SetBalanceAfter(wallet.Balance())
	if err := tx.TransitionToProcessed(now); err != nil {
		return err
	}
	if err := uc.wagerRepo.Create(ctx, tx); err != nil {
		return err
	}
	if err := uc.walletRepo.Update(ctx, wallet); err != nil {
		return err
	}
	if err := uc.ledgerRepo.Create(ctx, ledgerEntry); err != nil {
		return err
	}

	if err := uc.publishProcessedEvent(ctx, tx, wallet.ID(), now); err != nil {
		return err
	}
	if err := uc.publishBalanceChangedEvent(ctx, wallet, tx.ID(), ledgerEntry, now); err != nil {
		return err
	}

	*output = ProcessWagerOutput{
		TransactionID:    tx.ID(),
		Status:           tx.Status(),
		Balance:          wallet.Balance(),
		IdempotentReplay: false,
	}
	return nil
}

func (uc *ProcessWagerUseCase) deferToPendingReference(
	ctx context.Context,
	tx *domain.WagerTransaction,
	wallet *domain.Wallet,
	now time.Time,
	output *ProcessWagerOutput,
) error {
	tx.SetBalanceAfter(wallet.Balance())
	if err := tx.TransitionToPendingReference(now); err != nil {
		return err
	}
	if err := uc.wagerRepo.Create(ctx, tx); err != nil {
		return err
	}

	pendingEvent, err := domain.NewEventEnvelope(
		domain.EventTypeWagerTransactionPendingReference,
		wallet.ID(),
		uuid.NewString(),
		"",
		now,
		1,
		domain.WagerTransactionPendingReferenceData{
			TransactionID:       tx.ID(),
			ExternalID:          tx.ExternalID(),
			ProviderID:          tx.ProviderID(),
			WalletID:            tx.WalletID(),
			PlayerID:            tx.PlayerID(),
			Kind:                string(tx.Kind()),
			Amount:              tx.Amount(),
			ReferenceExternalID: tx.ReferenceExternalID(),
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create pending reference event: %w", err)
	}

	envelopeBytes, err := json.Marshal(pendingEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal pending event envelope: %w", err)
	}

	if err := uc.outboxRepo.Save(ctx, &database.OutboxRecord{
		EventID:       pendingEvent.EventID,
		EventType:     pendingEvent.EventType,
		AggregateID:   pendingEvent.AggregateID,
		CorrelationID: pendingEvent.CorrelationID,
		Payload:       envelopeBytes,
		CreatedAt:     now,
	}); err != nil {
		return fmt.Errorf("failed to save outbox event: %w", err)
	}

	*output = ProcessWagerOutput{
		TransactionID:    tx.ID(),
		Status:           tx.Status(),
		Balance:          wallet.Balance(),
		IdempotentReplay: false,
	}
	return nil
}

func (uc *ProcessWagerUseCase) rejectTransaction(
	ctx context.Context,
	tx *domain.WagerTransaction,
	wallet *domain.Wallet,
	failureCode string,
	now time.Time,
	output *ProcessWagerOutput,
) error {
	tx.SetBalanceAfter(wallet.Balance())
	if err := tx.TransitionToRejected(failureCode, now); err != nil {
		return err
	}
	if err := uc.wagerRepo.Create(ctx, tx); err != nil {
		return err
	}

	rejectedEvent, err := domain.NewEventEnvelope(
		domain.EventTypeWagerTransactionRejected,
		wallet.ID(),
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

	envelopeBytes, err := json.Marshal(rejectedEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal rejected event envelope: %w", err)
	}

	if err := uc.outboxRepo.Save(ctx, &database.OutboxRecord{
		EventID:       rejectedEvent.EventID,
		EventType:     rejectedEvent.EventType,
		AggregateID:   rejectedEvent.AggregateID,
		CorrelationID: rejectedEvent.CorrelationID,
		Payload:       envelopeBytes,
		CreatedAt:     now,
	}); err != nil {
		return fmt.Errorf("failed to save outbox event: %w", err)
	}

	*output = ProcessWagerOutput{
		TransactionID:    tx.ID(),
		Status:           tx.Status(),
		Balance:          wallet.Balance(),
		IdempotentReplay: false,
		FailureCode:      failureCode,
	}
	return nil
}

func (uc *ProcessWagerUseCase) publishProcessedEvent(
	ctx context.Context,
	tx *domain.WagerTransaction,
	walletID string,
	now time.Time,
) error {
	event, err := domain.NewEventEnvelope(
		domain.EventTypeWagerTransactionProcessed,
		walletID,
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
		return fmt.Errorf("failed to create processed event: %w", err)
	}

	envelopeBytes, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal processed event envelope: %w", err)
	}

	return uc.outboxRepo.Save(ctx, &database.OutboxRecord{
		EventID:       event.EventID,
		EventType:     event.EventType,
		AggregateID:   event.AggregateID,
		CorrelationID: event.CorrelationID,
		Payload:       envelopeBytes,
		CreatedAt:     now,
	})
}

func (uc *ProcessWagerUseCase) publishBalanceChangedEvent(
	ctx context.Context,
	wallet *domain.Wallet,
	txID string,
	entry domain.WalletLedgerEntry,
	now time.Time,
) error {
	event, err := domain.NewEventEnvelope(
		domain.EventTypeWalletBalanceChanged,
		wallet.ID(),
		uuid.NewString(),
		txID,
		now,
		1,
		domain.WalletBalanceChangedData{
			WalletID:      wallet.ID(),
			TransactionID: txID,
			Direction:     string(entry.Direction()),
			Money:         entry.Amount(),
			BalanceBefore: entry.BalanceBefore(),
			BalanceAfter:  entry.BalanceAfter(),
			WalletVersion: wallet.Version(),
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create balance changed event: %w", err)
	}

	envelopeBytes, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal balance changed event envelope: %w", err)
	}

	return uc.outboxRepo.Save(ctx, &database.OutboxRecord{
		EventID:       event.EventID,
		EventType:     event.EventType,
		AggregateID:   event.AggregateID,
		CorrelationID: event.CorrelationID,
		CausationID:   txID,
		Payload:       envelopeBytes,
		CreatedAt:     now,
	})
}
