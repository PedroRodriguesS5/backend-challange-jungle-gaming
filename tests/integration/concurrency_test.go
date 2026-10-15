//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"backend-challenge-go-jungle-gaming/internal/domain"
	"backend-challenge-go-jungle-gaming/internal/infra/messaging"
	"backend-challenge-go-jungle-gaming/internal/usecase"
)

func TestConcurrency_50IdenticalBets_SingleDebit(t *testing.T) {
	suite := setupTestSuite(t)
	ctx := context.Background()

	// 1. Create wallet with 1000.00 BRL
	initBal, _ := domain.NewMoney("1000.00", "BRL")
	walletOut, err := suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
		PlayerID:       "player-50-test",
		InitialBalance: initBal,
	})
	if err != nil {
		t.Fatalf("failed to create wallet: %v", err)
	}

	betAmt, _ := domain.NewMoney("25.00", "BRL")
	const concurrency = 50

	var (
		wg            sync.WaitGroup
		originalCount int32
		replayCount   int32
		errorsCount   int32
	)

	externalTxID := "ext-identical-tx-1"
	idempotencyKey := "key-identical-1"

	// 2. Launch 50 concurrent requests
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			out, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
				ProviderID:            "provider-a",
				ExternalTransactionID: externalTxID,
				IdempotencyKey:        idempotencyKey,
				PlayerID:              walletOut.PlayerID,
				WalletID:              walletOut.ID,
				RoundID:               "round-1",
				GameID:                "fortune-tiger",
				Kind:                  domain.KindBet,
				Money:                 betAmt,
			})
			if err != nil {
				atomic.AddInt32(&errorsCount, 1)
				return
			}
			if out.Status == domain.StatusProcessed {
				if out.IdempotentReplay {
					atomic.AddInt32(&replayCount, 1)
				} else {
					atomic.AddInt32(&originalCount, 1)
				}
			}
		}()
	}

	wg.Wait()

	if errorsCount > 0 {
		t.Fatalf("expected 0 errors, got %d", errorsCount)
	}
	if originalCount != 1 {
		t.Fatalf("expected exactly 1 original execution, got %d", originalCount)
	}
	if replayCount != concurrency-1 {
		t.Fatalf("expected %d replays, got %d", concurrency-1, replayCount)
	}

	// 3. Verify final balance is 975.00 BRL
	w, err := suite.GetWalletUC.Execute(ctx, walletOut.ID)
	if err != nil {
		t.Fatalf("failed to get wallet: %v", err)
	}
	if w.Balance().AmountString() != "975.00" {
		t.Fatalf("expected final balance 975.00, got %s", w.Balance().AmountString())
	}

	// 4. Verify ledger has exactly 2 entries (1 opening + 1 debit)
	entries, _, err := suite.LedgerRepo.ListByWalletID(ctx, walletOut.ID, "", 10)
	if err != nil {
		t.Fatalf("failed to list ledger: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected exactly 2 ledger entries, got %d", len(entries))
	}
}

func TestConcurrency_Two80BetsOn100Balance_RaceCondition(t *testing.T) {
	suite := setupTestSuite(t)
	ctx := context.Background()

	// 1. Create wallet with 100.00 BRL
	initBal, _ := domain.NewMoney("100.00", "BRL")
	walletOut, err := suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
		PlayerID:       "player-race-test",
		InitialBalance: initBal,
	})
	if err != nil {
		t.Fatalf("failed to create wallet: %v", err)
	}

	betAmt, _ := domain.NewMoney("80.00", "BRL")

	var (
		wg             sync.WaitGroup
		processedCount int32
		rejectedCount  int32
	)

	startBarrier := make(chan struct{})
	wg.Add(2)

	// Bet 1
	go func() {
		defer wg.Done()
		<-startBarrier
		out, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
			ProviderID:            "provider-a",
			ExternalTransactionID: "race-tx-1",
			IdempotencyKey:        "key-race-1",
			PlayerID:              walletOut.PlayerID,
			WalletID:              walletOut.ID,
			RoundID:               "round-1",
			GameID:                "fortune-tiger",
			Kind:                  domain.KindBet,
			Money:                 betAmt,
		})
		if err == nil {
			if out.Status == domain.StatusProcessed {
				atomic.AddInt32(&processedCount, 1)
			} else if out.Status == domain.StatusRejected && out.FailureCode == domain.FailureCodeInsufficientFunds {
				atomic.AddInt32(&rejectedCount, 1)
			}
		}
	}()

	// Bet 2
	go func() {
		defer wg.Done()
		<-startBarrier
		out, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
			ProviderID:            "provider-a",
			ExternalTransactionID: "race-tx-2",
			IdempotencyKey:        "key-race-2",
			PlayerID:              walletOut.PlayerID,
			WalletID:              walletOut.ID,
			RoundID:               "round-1",
			GameID:                "fortune-tiger",
			Kind:                  domain.KindBet,
			Money:                 betAmt,
		})
		if err == nil {
			if out.Status == domain.StatusProcessed {
				atomic.AddInt32(&processedCount, 1)
			} else if out.Status == domain.StatusRejected && out.FailureCode == domain.FailureCodeInsufficientFunds {
				atomic.AddInt32(&rejectedCount, 1)
			}
		}
	}()

	close(startBarrier)
	wg.Wait()

	if processedCount != 1 {
		t.Fatalf("expected exactly 1 processed bet, got %d", processedCount)
	}
	if rejectedCount != 1 {
		t.Fatalf("expected exactly 1 rejected bet, got %d", rejectedCount)
	}

	// Final balance must be exactly 20.00 BRL
	w, err := suite.GetWalletUC.Execute(ctx, walletOut.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Balance().AmountString() != "20.00" {
		t.Fatalf("expected final balance 20.00, got %s", w.Balance().AmountString())
	}

	// Ledger must contain exactly 1 debit entry (+ 1 opening entry)
	entries, _, err := suite.LedgerRepo.ListByWalletID(ctx, walletOut.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected exactly 2 ledger entries, got %d", len(entries))
	}
}

func TestConcurrency_ParallelDistinctWallets(t *testing.T) {
	suite := setupTestSuite(t)
	ctx := context.Background()

	const walletCount = 5
	walletIDs := make([]string, walletCount)
	initBal, _ := domain.NewMoney("500.00", "BRL")

	for i := 0; i < walletCount; i++ {
		out, err := suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
			PlayerID:       fmt.Sprintf("multi-player-%d", i),
			InitialBalance: initBal,
		})
		if err != nil {
			t.Fatalf("failed to create wallet %d: %v", i, err)
		}
		walletIDs[i] = out.ID
	}

	betAmt, _ := domain.NewMoney("50.00", "BRL")
	var wg sync.WaitGroup
	wg.Add(walletCount)

	for i := 0; i < walletCount; i++ {
		idx := i
		go func() {
			defer wg.Done()
			_, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
				ProviderID:            "provider-a",
				ExternalTransactionID: fmt.Sprintf("tx-wallet-%d", idx),
				IdempotencyKey:        fmt.Sprintf("key-wallet-%d", idx),
				PlayerID:              fmt.Sprintf("multi-player-%d", idx),
				WalletID:              walletIDs[idx],
				RoundID:               "round-multi",
				GameID:                "fortune-ox",
				Kind:                  domain.KindBet,
				Money:                 betAmt,
			})
			if err != nil {
				t.Errorf("bet failed for wallet %d: %v", idx, err)
			}
		}()
	}

	wg.Wait()

	for i := 0; i < walletCount; i++ {
		w, err := suite.GetWalletUC.Execute(ctx, walletIDs[i])
		if err != nil {
			t.Fatal(err)
		}
		if w.Balance().AmountString() != "450.00" {
			t.Errorf("expected balance 450.00 for wallet %d, got %s", i, w.Balance().AmountString())
		}
	}
}

func TestAppendOnlyLedger_ImmutabilityTrigger(t *testing.T) {
	suite := setupTestSuite(t)
	ctx := context.Background()

	initBal, _ := domain.NewMoney("100.00", "BRL")
	walletOut, err := suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
		PlayerID:       "player-append-only",
		InitialBalance: initBal,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Attempt to UPDATE ledger entry
	_, err = suite.Pool.Exec(ctx, "UPDATE wallet_ledger_entries SET amount = 999.00 WHERE wallet_id = $1", walletOut.ID)
	if err == nil {
		t.Fatal("expected error on UPDATE to wallet_ledger_entries, got nil")
	}

	// Attempt to DELETE ledger entry
	_, err = suite.Pool.Exec(ctx, "DELETE FROM wallet_ledger_entries WHERE wallet_id = $1", walletOut.ID)
	if err == nil {
		t.Fatal("expected error on DELETE from wallet_ledger_entries, got nil")
	}
}

func TestPendingReference_RefundResolvesWhenBetArrives(t *testing.T) {
	suite := setupTestSuite(t)
	ctx := context.Background()

	initBal, _ := domain.NewMoney("100.00", "BRL")
	walletOut, err := suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
		PlayerID:       "player-pending-ref",
		InitialBalance: initBal,
	})
	if err != nil {
		t.Fatal(err)
	}

	betAmt, _ := domain.NewMoney("30.00", "BRL")

	// 1. REFUND arrives BEFORE the BET
	refundOut, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:                     "provider-a",
		ExternalTransactionID:          "refund-early-1",
		IdempotencyKey:                 "key-refund-1",
		PlayerID:                       walletOut.PlayerID,
		WalletID:                       walletOut.ID,
		RoundID:                        "round-ref-1",
		GameID:                         "fortune-rabbit",
		Kind:                           domain.KindRefund,
		Money:                          betAmt,
		ReferenceExternalTransactionID: "bet-original-1",
	})
	if err != nil {
		t.Fatalf("unexpected error on early refund: %v", err)
	}
	if refundOut.Status != domain.StatusPendingReference {
		t.Fatalf("expected PENDING_REFERENCE, got %s", refundOut.Status)
	}

	// 2. Original BET arrives and is processed
	betOut, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "bet-original-1",
		IdempotencyKey:        "key-bet-1",
		PlayerID:              walletOut.PlayerID,
		WalletID:              walletOut.ID,
		RoundID:               "round-ref-1",
		GameID:                "fortune-rabbit",
		Kind:                  domain.KindBet,
		Money:                 betAmt,
	})
	if err != nil {
		t.Fatalf("failed to process original bet: %v", err)
	}
	if betOut.Status != domain.StatusProcessed {
		t.Fatalf("expected BET to be PROCESSED, got %s", betOut.Status)
	}

	// 3. Trigger pending reference retry worker
	worker := messaging.NewPendingReferenceWorker(
		suite.TxManager,
		suite.WagerRepo,
		suite.WalletRepo,
		suite.LedgerRepo,
		suite.OutboxRepo,
		nil,
	)

	// Simulate one retry pass
	pending, err := suite.WagerRepo.FindPendingReferences(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending reference, got %d", len(pending))
	}

	// Start worker briefly to process pending batch
	workerCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	worker.Start(workerCtx)
	time.Sleep(2500 * time.Millisecond)
	worker.Stop()

	// 4. Verify REFUND has transitioned to PROCESSED
	refundTx, err := suite.WagerRepo.FindByProviderAndExternalID(ctx, "provider-a", "refund-early-1")
	if err != nil {
		t.Fatal(err)
	}
	if refundTx.Status() != domain.StatusProcessed {
		t.Fatalf("expected REFUND to be PROCESSED after retry, got %s", refundTx.Status())
	}

	// 5. Final balance: 100 - 30 + 30 = 100.00 BRL
	w, err := suite.GetWalletUC.Execute(ctx, walletOut.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Balance().AmountString() != "100.00" {
		t.Fatalf("expected final balance 100.00, got %s", w.Balance().AmountString())
	}
}

func TestWalletReconciliation(t *testing.T) {
	suite := setupTestSuite(t)
	ctx := context.Background()

	initBal, _ := domain.NewMoney("50.00", "BRL")
	walletOut, err := suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
		PlayerID:       "player-reconcile",
		InitialBalance: initBal,
	})
	if err != nil {
		t.Fatal(err)
	}

	betAmt, _ := domain.NewMoney("20.00", "BRL")
	_, err = suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "rec-bet-1",
		IdempotencyKey:        "rec-key-1",
		PlayerID:              walletOut.PlayerID,
		WalletID:              walletOut.ID,
		RoundID:               "round-1",
		GameID:                "fortune-mouse",
		Kind:                  domain.KindBet,
		Money:                 betAmt,
	})
	if err != nil {
		t.Fatal(err)
	}

	winAmt, _ := domain.NewMoney("70.00", "BRL")
	_, err = suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "rec-win-1",
		IdempotencyKey:        "rec-key-2",
		PlayerID:              walletOut.PlayerID,
		WalletID:              walletOut.ID,
		RoundID:               "round-1",
		GameID:                "fortune-mouse",
		Kind:                  domain.KindWin,
		Money:                 winAmt,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 50 (opening) - 20 (bet) + 70 (win) = 100.00
	recOut, err := suite.ReconcileWalletUC.Execute(ctx, walletOut.ID)
	if err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	if !recOut.Consistent {
		t.Errorf("expected reconciliation to be consistent")
	}
	if recOut.StoredBalance.AmountString() != "100.00" {
		t.Errorf("expected stored balance 100.00, got %s", recOut.StoredBalance.AmountString())
	}
	if recOut.CalculatedBalance.AmountString() != "100.00" {
		t.Errorf("expected calculated balance 100.00, got %s", recOut.CalculatedBalance.AmountString())
	}
	if recOut.Difference.AmountString() != "0.00" {
		t.Errorf("expected difference 0.00, got %s", recOut.Difference.AmountString())
	}
	if recOut.CheckedEntries != 3 {
		t.Errorf("expected 3 checked entries, got %d", recOut.CheckedEntries)
	}
}
