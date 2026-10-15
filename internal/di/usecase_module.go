package di

import (
	"backend-challenge-go-jungle-gaming/internal/infra/database"
	"backend-challenge-go-jungle-gaming/internal/usecase"
	"go.uber.org/fx"
)

var UsecaseModule = fx.Module("usecase",
	fx.Provide(
		provideCreateWalletUseCase,
		provideGetWalletUseCase,
		provideListLedgerUseCase,
		provideProcessWagerUseCase,
		provideGetTransactionUseCase,
		provideReconcileWalletUseCase,
	),
)

func provideCreateWalletUseCase(
	txManager database.TxManager,
	walletRepo database.WalletRepository,
	wagerRepo database.WagerRepository,
	ledgerRepo database.LedgerRepository,
	outboxRepo database.OutboxRepository,
) *usecase.CreateWalletUseCase {
	return usecase.NewCreateWalletUseCase(txManager, walletRepo, wagerRepo, ledgerRepo, outboxRepo)
}

func provideGetWalletUseCase(walletRepo database.WalletRepository) *usecase.GetWalletUseCase {
	return usecase.NewGetWalletUseCase(walletRepo)
}

func provideListLedgerUseCase(ledgerRepo database.LedgerRepository) *usecase.ListLedgerUseCase {
	return usecase.NewListLedgerUseCase(ledgerRepo)
}

func provideProcessWagerUseCase(
	txManager database.TxManager,
	walletRepo database.WalletRepository,
	wagerRepo database.WagerRepository,
	ledgerRepo database.LedgerRepository,
	outboxRepo database.OutboxRepository,
) *usecase.ProcessWagerUseCase {
	return usecase.NewProcessWagerUseCase(txManager, walletRepo, wagerRepo, ledgerRepo, outboxRepo)
}

func provideGetTransactionUseCase(wagerRepo database.WagerRepository) *usecase.GetTransactionUseCase {
	return usecase.NewGetTransactionUseCase(wagerRepo)
}

func provideReconcileWalletUseCase(
	txManager database.TxManager,
	walletRepo database.WalletRepository,
	ledgerRepo database.LedgerRepository,
) *usecase.ReconcileWalletUseCase {
	return usecase.NewReconcileWalletUseCase(txManager, walletRepo, ledgerRepo)
}
