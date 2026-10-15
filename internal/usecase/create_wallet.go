package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"backend-challenge-go-jungle-gaming/internal/domain"
	"backend-challenge-go-jungle-gaming/internal/infra/database"
	"github.com/google/uuid"
)

type CreateWalletInput struct {
	PlayerID       string
	InitialBalance domain.Money
}

type CreateWalletOutput struct {
	ID       string       `json:"id"`
	PlayerID string       `json:"playerId"`
	Balance  domain.Money `json:"balance"`
	Version  int64        `json:"version"`
}

type CreateWalletUseCase struct {
	txManager  database.TxManager
	walletRepo database.WalletRepository
	wagerRepo  database.WagerRepository
	ledgerRepo database.LedgerRepository
	outboxRepo database.OutboxRepository
}

func NewCreateWalletUseCase(
	txManager database.TxManager,
	walletRepo database.WalletRepository,
	wagerRepo database.WagerRepository,
	ledgerRepo database.LedgerRepository,
	outboxRepo database.OutboxRepository,
) *CreateWalletUseCase {
	return &CreateWalletUseCase{
		txManager:  txManager,
		walletRepo: walletRepo,
		wagerRepo:  wagerRepo,
		ledgerRepo: ledgerRepo,
		outboxRepo: outboxRepo,
	}
}

func (uc *CreateWalletUseCase) Execute(ctx context.Context, input CreateWalletInput) (*CreateWalletOutput, error) {
	now := time.Now().UTC()
	walletID := uuid.NewString()

	wallet, err := domain.NewWallet(walletID, input.PlayerID, input.InitialBalance, now)
	if err != nil {
		return nil, err
	}

	var output CreateWalletOutput

	err = uc.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		existing, err := uc.walletRepo.FindByPlayerIDAndCurrency(txCtx, input.PlayerID, input.InitialBalance.Currency())
		if err == nil && existing != nil {
			return domain.NewDomainError(
				domain.FailureCodeIdempotencyConflict,
				"wallet already exists for this player and currency",
				nil,
			)
		}

		if err := uc.walletRepo.Create(txCtx, wallet); err != nil {
			return err
		}

		if input.InitialBalance.IsPositive() {
			openingTxID := uuid.NewString()
			openingTx, err := domain.NewInternalOpeningTransaction(
				openingTxID,
				wallet.ID(),
				wallet.PlayerID(),
				wallet.Balance(),
				now,
			)
			if err != nil {
				return err
			}

			if err := uc.wagerRepo.Create(txCtx, openingTx); err != nil {
				return err
			}

			ledgerEntryID := uuid.NewString()
			ledgerEntry, err := wallet.CreateOpeningLedgerEntry(openingTxID, ledgerEntryID, now)
			if err != nil {
				return err
			}

			if err := uc.ledgerRepo.Create(txCtx, ledgerEntry); err != nil {
				return err
			}

			corrID := uuid.NewString()
			processedEvent, err := domain.NewEventEnvelope(
				domain.EventTypeWagerTransactionProcessed,
				wallet.ID(),
				corrID,
				"",
				now,
				1,
				domain.WagerTransactionProcessedData{
					TransactionID: openingTx.ID(),
					ExternalID:    openingTx.ExternalID(),
					ProviderID:    openingTx.ProviderID(),
					WalletID:      wallet.ID(),
					PlayerID:      wallet.PlayerID(),
					Kind:          string(openingTx.Kind()),
					Amount:        openingTx.Amount(),
					Status:        string(openingTx.Status()),
				},
			)
			if err != nil {
				return fmt.Errorf("failed to create processed event: %w", err)
			}

			processedPayload, err := json.Marshal(processedEvent)
			if err != nil {
				return fmt.Errorf("failed to marshal processed event envelope: %w", err)
			}

			if err := uc.outboxRepo.Save(txCtx, &database.OutboxRecord{
				EventID:       processedEvent.EventID,
				EventType:     processedEvent.EventType,
				AggregateID:   processedEvent.AggregateID,
				CorrelationID: processedEvent.CorrelationID,
				Payload:       processedPayload,
				CreatedAt:     now,
			}); err != nil {
				return fmt.Errorf("failed to save outbox event: %w", err)
			}

			balanceChangedEvent, err := domain.NewEventEnvelope(
				domain.EventTypeWalletBalanceChanged,
				wallet.ID(),
				corrID,
				openingTx.ID(),
				now,
				1,
				domain.WalletBalanceChangedData{
					WalletID:      wallet.ID(),
					TransactionID: openingTx.ID(),
					Direction:     string(domain.DirectionCredit),
					Money:         wallet.Balance(),
					BalanceBefore: domain.ZeroMoney(wallet.Currency()),
					BalanceAfter:  wallet.Balance(),
					WalletVersion: wallet.Version(),
				},
			)
			if err != nil {
				return fmt.Errorf("failed to create balance changed event: %w", err)
			}

			balanceChangedPayload, err := json.Marshal(balanceChangedEvent)
			if err != nil {
				return fmt.Errorf("failed to marshal balance changed event envelope: %w", err)
			}

			if err := uc.outboxRepo.Save(txCtx, &database.OutboxRecord{
				EventID:       balanceChangedEvent.EventID,
				EventType:     balanceChangedEvent.EventType,
				AggregateID:   balanceChangedEvent.AggregateID,
				CorrelationID: balanceChangedEvent.CorrelationID,
				CausationID:   openingTx.ID(),
				Payload:       balanceChangedPayload,
				CreatedAt:     now,
			}); err != nil {
				return fmt.Errorf("failed to save outbox event: %w", err)
			}
		}

		output = CreateWalletOutput{
			ID:       wallet.ID(),
			PlayerID: wallet.PlayerID(),
			Balance:  wallet.Balance(),
			Version:  wallet.Version(),
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return &output, nil
}
