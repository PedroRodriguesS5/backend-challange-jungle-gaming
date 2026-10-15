//go:build integration

package integration_test

import (
	"context"
	"errors"
	"testing"

	"backend-challenge-go-jungle-gaming/internal/domain"
	"backend-challenge-go-jungle-gaming/internal/usecase"
)

func TestMultiCurrency_SamePlayerDifferentCurrencies(t *testing.T) {
	suite := setupTestSuite(t)
	ctx := context.Background()
	playerID := "player-multi-currency-1"

	// 1. Create BRL wallet for player-1
	balBRL, _ := domain.NewMoney("500.00", "BRL")
	walletBRL, err := suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
		PlayerID:       playerID,
		InitialBalance: balBRL,
	})
	if err != nil {
		t.Fatalf("failed to create BRL wallet: %v", err)
	}

	// 2. Create USD wallet for the same player-1 -> Must succeed with distinct walletId
	balUSD, _ := domain.NewMoney("100.00", "USD")
	walletUSD, err := suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
		PlayerID:       playerID,
		InitialBalance: balUSD,
	})
	if err != nil {
		t.Fatalf("failed to create USD wallet for same player: %v", err)
	}

	if walletBRL.ID == walletUSD.ID {
		t.Fatalf("expected distinct wallet IDs for different currencies, got same: %s", walletBRL.ID)
	}
	if walletBRL.Balance.Currency() != "BRL" {
		t.Errorf("expected BRL, got %s", walletBRL.Balance.Currency())
	}
	if walletUSD.Balance.Currency() != "USD" {
		t.Errorf("expected USD, got %s", walletUSD.Balance.Currency())
	}

	// 3. Attempt to create a duplicate BRL wallet for same player-1 -> Must be rejected with conflict
	_, err = suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
		PlayerID:       playerID,
		InitialBalance: balBRL,
	})
	if err == nil {
		t.Fatal("expected error creating duplicate (playerId, currency) wallet, got nil")
	}

	var domErr *domain.DomainError
	if !errors.As(err, &domErr) || domErr.Code != domain.FailureCodeIdempotencyConflict {
		t.Fatalf("expected FailureCodeIdempotencyConflict, got %v", err)
	}
}

func TestMultiCurrency_RejectWagerCurrencyMismatch(t *testing.T) {
	suite := setupTestSuite(t)
	ctx := context.Background()
	playerID := "player-currency-mismatch"

	// Create BRL wallet
	balBRL, _ := domain.NewMoney("200.00", "BRL")
	walletBRL, err := suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
		PlayerID:       playerID,
		InitialBalance: balBRL,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Attempt a BET in USD on a BRL wallet -> Must be rejected with currency mismatch
	betUSD, _ := domain.NewMoney("15.00", "USD")
	_, err = suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "bet-mismatch-1",
		IdempotencyKey:        "key-mismatch-1",
		PlayerID:              playerID,
		WalletID:              walletBRL.ID,
		RoundID:               "round-1",
		GameID:                "fortune-tiger",
		Kind:                  domain.KindBet,
		Money:                 betUSD,
	})
	if err == nil {
		t.Fatal("expected error for currency mismatch, got nil")
	}

	var domErr *domain.DomainError
	if !errors.As(err, &domErr) || domErr.Code != domain.FailureCodeCurrencyMismatch {
		t.Fatalf("expected FailureCodeCurrencyMismatch, got %v", err)
	}

	// Verify wallet balance remained intact
	w, err := suite.GetWalletUC.Execute(ctx, walletBRL.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Balance().AmountString() != "200.00" {
		t.Errorf("balance should remain 200.00, got %s", w.Balance().AmountString())
	}
}

func TestMultiCurrency_RejectReversalCrossWalletAndCurrency(t *testing.T) {
	suite := setupTestSuite(t)
	ctx := context.Background()
	playerID := "player-cross-reversal"

	// Create BRL and USD wallets
	balBRL, _ := domain.NewMoney("100.00", "BRL")
	walletBRL, _ := suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
		PlayerID:       playerID,
		InitialBalance: balBRL,
	})

	balUSD, _ := domain.NewMoney("100.00", "USD")
	walletUSD, _ := suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
		PlayerID:       playerID,
		InitialBalance: balUSD,
	})

	// Process BET on BRL wallet
	betBRL, _ := domain.NewMoney("25.00", "BRL")
	betOut, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "bet-orig-brl",
		IdempotencyKey:        "key-orig-brl",
		PlayerID:              playerID,
		WalletID:              walletBRL.ID,
		RoundID:               "round-1",
		GameID:                "fortune-tiger",
		Kind:                  domain.KindBet,
		Money:                 betBRL,
	})
	if err != nil {
		t.Fatalf("failed to process original BRL bet: %v", err)
	}
	if betOut.Status != domain.StatusProcessed {
		t.Fatalf("expected original bet to be PROCESSED, got %s", betOut.Status)
	}

	// Attempt REFUND targeting the USD wallet referencing the BRL bet
	betUSD, _ := domain.NewMoney("25.00", "USD")
	refundOut, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:                     "provider-a",
		ExternalTransactionID:          "refund-cross-1",
		IdempotencyKey:                 "key-cross-1",
		PlayerID:                       playerID,
		WalletID:                       walletUSD.ID, // wrong wallet!
		RoundID:                        "round-1",
		GameID:                         "fortune-tiger",
		Kind:                           domain.KindRefund,
		Money:                          betUSD, // wrong currency!
		ReferenceExternalTransactionID: "bet-orig-brl",
	})
	if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}

	// Must be rejected due to reference mismatch (different wallet & currency)
	if refundOut.Status != domain.StatusRejected {
		t.Fatalf("expected cross-wallet/currency refund to be REJECTED, got %s", refundOut.Status)
	}
	if refundOut.FailureCode != domain.FailureCodeReferenceMismatch {
		t.Errorf("expected FailureCodeReferenceMismatch, got %s", refundOut.FailureCode)
	}
}

func TestB2BProviderIsolation_FullLifecycle(t *testing.T) {
	suite := setupTestSuite(t)
	ctx := context.Background()
	playerID := "player-b2b-test"

	balBRL, _ := domain.NewMoney("500.00", "BRL")
	wallet, err := suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
		PlayerID:       playerID,
		InitialBalance: balBRL,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Provider A executes a bet
	betAmt, _ := domain.NewMoney("50.00", "BRL")
	betOutA, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "tx-shared-ext-id",
		IdempotencyKey:        "provider-a:key-1",
		PlayerID:              playerID,
		WalletID:              wallet.ID,
		RoundID:               "round-1",
		GameID:                "game-1",
		Kind:                  domain.KindBet,
		Money:                 betAmt,
	})
	if err != nil {
		t.Fatalf("provider-a bet failed: %v", err)
	}
	if betOutA.Status != domain.StatusProcessed {
		t.Fatalf("expected provider-a bet to be PROCESSED, got %s", betOutA.Status)
	}

	// 2. Provider B attempts to send an operation with the same external ID ("tx-shared-ext-id")
	// Since operations are isolated by (provider_id, external_id), Provider B is NOT conflicting
	// and does NOT get Provider A's idempotent replay. It creates Provider B's own independent transaction.
	betOutB, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:            "provider-b",
		ExternalTransactionID: "tx-shared-ext-id",
		IdempotencyKey:        "provider-b:key-1",
		PlayerID:              playerID,
		WalletID:              wallet.ID,
		RoundID:               "round-1",
		GameID:                "game-1",
		Kind:                  domain.KindBet,
		Money:                 betAmt,
	})
	if err != nil {
		t.Fatalf("provider-b bet failed: %v", err)
	}
	if betOutB.IdempotentReplay {
		t.Fatal("provider-b must not receive an idempotent replay for provider-a's transaction")
	}
	if betOutB.TransactionID == betOutA.TransactionID {
		t.Fatal("provider-b must receive a distinct transaction ID")
	}

	// 3. Provider B attempts to REFUND Provider A's unique bet ("tx-provider-a-only")
	// Provider A created tx-provider-a-only, but Provider B has no such transaction.
	_, err = suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "tx-provider-a-only",
		IdempotencyKey:        "provider-a:key-only",
		PlayerID:              playerID,
		WalletID:              wallet.ID,
		RoundID:               "round-1",
		GameID:                "game-1",
		Kind:                  domain.KindBet,
		Money:                 betAmt,
	})
	if err != nil {
		t.Fatal(err)
	}

	refundOutB, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:                     "provider-b",
		ExternalTransactionID:          "refund-illegal-by-b",
		IdempotencyKey:                 "provider-b:key-refund-1",
		PlayerID:                       playerID,
		WalletID:                       wallet.ID,
		RoundID:                        "round-1",
		GameID:                         "game-1",
		Kind:                           domain.KindRefund,
		Money:                          betAmt,
		ReferenceExternalTransactionID: "tx-provider-a-only", // Provider B referencing Provider A's bet
	})
	if err != nil {
		t.Fatalf("unexpected error on provider-b refund: %v", err)
	}

	// Provider B does not own "tx-provider-a-only". Under provider-b's scope,
	// the reference is not found in provider-b's data, so it remains PENDING_REFERENCE or REJECTED.
	// It must NEVER be processed as a valid refund under Provider B!
	if refundOutB.Status == domain.StatusProcessed {
		t.Fatal("provider-b must NOT be able to refund provider-a's transaction")
	}

	// 4. Provider B query isolation:
	// Verify FindByProviderAndExternalID does not leak Provider A's transaction to Provider B
	txAFromBView, err := suite.WagerRepo.FindByProviderAndExternalID(ctx, "provider-b", "tx-provider-a-only")
	if err != nil && !errors.Is(err, domain.ErrTransactionNotFound) {
		t.Fatalf("unexpected error querying provider-a from provider-b: %v", err)
	}
	if txAFromBView != nil {
		t.Fatal("data leak: provider-b was able to read provider-a's transaction!")
	}
}
