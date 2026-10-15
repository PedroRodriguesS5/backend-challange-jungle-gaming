package usecase

import (
	"context"
	"log/slog"

	"backend-challenge-go-jungle-gaming/internal/domain"
	"backend-challenge-go-jungle-gaming/internal/infra/database"
	"backend-challenge-go-jungle-gaming/internal/infra/metrics"
)

type GetWalletUseCase struct {
	walletRepo database.WalletRepository
}

func NewGetWalletUseCase(walletRepo database.WalletRepository) *GetWalletUseCase {
	return &GetWalletUseCase{walletRepo: walletRepo}
}

func (uc *GetWalletUseCase) Execute(ctx context.Context, walletID string) (*domain.Wallet, error) {
	return uc.walletRepo.FindByID(ctx, walletID)
}

type ListLedgerUseCase struct {
	ledgerRepo database.LedgerRepository
}

func NewListLedgerUseCase(ledgerRepo database.LedgerRepository) *ListLedgerUseCase {
	return &ListLedgerUseCase{ledgerRepo: ledgerRepo}
}

type ListLedgerOutput struct {
	Entries    []domain.WalletLedgerEntry `json:"entries"`
	NextCursor string                     `json:"nextCursor,omitempty"`
}

func (uc *ListLedgerUseCase) Execute(ctx context.Context, walletID string, cursor string, limit int) (*ListLedgerOutput, error) {
	entries, nextCursor, err := uc.ledgerRepo.ListByWalletID(ctx, walletID, cursor, limit)
	if err != nil {
		return nil, err
	}
	return &ListLedgerOutput{
		Entries:    entries,
		NextCursor: nextCursor,
	}, nil
}

type GetTransactionUseCase struct {
	wagerRepo database.WagerRepository
}

func NewGetTransactionUseCase(wagerRepo database.WagerRepository) *GetTransactionUseCase {
	return &GetTransactionUseCase{wagerRepo: wagerRepo}
}

func (uc *GetTransactionUseCase) GetByID(ctx context.Context, id string) (*domain.WagerTransaction, error) {
	return uc.wagerRepo.FindByID(ctx, id)
}

func (uc *GetTransactionUseCase) GetByProviderAndExternalID(ctx context.Context, providerID string, externalID string) (*domain.WagerTransaction, error) {
	return uc.wagerRepo.FindByProviderAndExternalID(ctx, providerID, externalID)
}

type ReconcileWalletUseCase struct {
	txManager  database.TxManager
	walletRepo database.WalletRepository
	ledgerRepo database.LedgerRepository
}

func NewReconcileWalletUseCase(
	txManager database.TxManager,
	walletRepo database.WalletRepository,
	ledgerRepo database.LedgerRepository,
) *ReconcileWalletUseCase {
	return &ReconcileWalletUseCase{
		txManager:  txManager,
		walletRepo: walletRepo,
		ledgerRepo: ledgerRepo,
	}
}

type ReconciliationOutput struct {
	WalletID          string       `json:"walletId"`
	StoredBalance     domain.Money `json:"storedBalance"`
	CalculatedBalance domain.Money `json:"calculatedBalance"`
	Difference        domain.Money `json:"difference"`
	Consistent        bool         `json:"consistent"`
	CheckedEntries    int          `json:"checkedEntries"`
}

func (uc *ReconcileWalletUseCase) Execute(ctx context.Context, walletID string) (*ReconciliationOutput, error) {
	var output ReconciliationOutput

	err := uc.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		wallet, err := uc.walletRepo.FindByID(txCtx, walletID)
		if err != nil {
			return err
		}

		calculatedDec, count, err := uc.ledgerRepo.CalculateBalance(txCtx, walletID)
		if err != nil {
			return err
		}

		calcMoney, err := domain.NewMoneyFromDecimal(calculatedDec, wallet.Currency())
		if err != nil {
			return err
		}

		diffDec := wallet.Balance().Amount().Sub(calculatedDec)
		diffMoney, err := domain.NewMoneyFromDecimal(diffDec, wallet.Currency())
		if err != nil {
			return err
		}

		consistent := wallet.Balance().Equal(calcMoney)
		if !consistent {
			slog.Error("ledger reconciliation divergence detected",
				slog.String("wallet_id", walletID),
				slog.String("stored_balance", wallet.Balance().AmountString()),
				slog.String("calculated_balance", calcMoney.AmountString()),
				slog.String("difference", diffMoney.AmountString()),
			)
			metrics.IncReconciliationDivergences()
		}

		output = ReconciliationOutput{
			WalletID:          walletID,
			StoredBalance:     wallet.Balance(),
			CalculatedBalance: calcMoney,
			Difference:        diffMoney,
			Consistent:        consistent,
			CheckedEntries:    count,
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return &output, nil
}
