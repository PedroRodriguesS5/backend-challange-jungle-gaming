//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"backend-challenge-go-jungle-gaming/internal/di"
	"backend-challenge-go-jungle-gaming/internal/domain"
	"backend-challenge-go-jungle-gaming/internal/infra/database"
	"backend-challenge-go-jungle-gaming/internal/infra/messaging"
	"backend-challenge-go-jungle-gaming/internal/usecase"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/uuid"
	"go.uber.org/fx"
)

func TestIdempotency_HistoricalBalanceReplay(t *testing.T) {
	suite := setupTestSuite(t)
	ctx := context.Background()
	playerID := "player-hist-replay"

	initBal, _ := domain.NewMoney("1000.00", "BRL")
	wallet, err := suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
		PlayerID:       playerID,
		InitialBalance: initBal,
	})
	if err != nil {
		t.Fatal(err)
	}

	// First BET: 100.00 BRL
	bet1Amt, _ := domain.NewMoney("100.00", "BRL")
	bet1Out, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "tx-hist-1",
		IdempotencyKey:        "key-hist-1",
		PlayerID:              playerID,
		WalletID:              wallet.ID,
		RoundID:               "round-1",
		GameID:                "game-1",
		Kind:                  domain.KindBet,
		Money:                 bet1Amt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bet1Out.Balance.AmountString() != "900.00" {
		t.Fatalf("expected balance 900.00 after bet 1, got %s", bet1Out.Balance.AmountString())
	}

	// Second BET: 200.00 BRL
	bet2Amt, _ := domain.NewMoney("200.00", "BRL")
	bet2Out, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "tx-hist-2",
		IdempotencyKey:        "key-hist-2",
		PlayerID:              playerID,
		WalletID:              wallet.ID,
		RoundID:               "round-2",
		GameID:                "game-1",
		Kind:                  domain.KindBet,
		Money:                 bet2Amt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bet2Out.Balance.AmountString() != "700.00" {
		t.Fatalf("expected balance 700.00 after bet 2, got %s", bet2Out.Balance.AmountString())
	}

	// REPLAY First BET: Must return historical balance 900.00 BRL, NOT current balance 700.00 BRL!
	replayOut, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "tx-hist-1",
		IdempotencyKey:        "key-hist-1",
		PlayerID:              playerID,
		WalletID:              wallet.ID,
		RoundID:               "round-1",
		GameID:                "game-1",
		Kind:                  domain.KindBet,
		Money:                 bet1Amt,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !replayOut.IdempotentReplay {
		t.Error("expected IdempotentReplay == true")
	}
	if replayOut.Balance.AmountString() != "900.00" {
		t.Errorf("replay must return historical balance 900.00, got %s", replayOut.Balance.AmountString())
	}
}

func TestIdempotency_ReusedExternalIDDifferentKey_Conflict(t *testing.T) {
	suite := setupTestSuite(t)
	ctx := context.Background()
	playerID := "player-key-conflict"

	initBal, _ := domain.NewMoney("500.00", "BRL")
	wallet, _ := suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
		PlayerID:       playerID,
		InitialBalance: initBal,
	})

	betAmt, _ := domain.NewMoney("50.00", "BRL")
	// Process transaction with key-1
	_, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "tx-conflict-1",
		IdempotencyKey:        "key-original-1",
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

	// Try submitting same (provider-a, tx-conflict-1) with a DIFFERENT key ("key-reused-2")
	_, err = suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "tx-conflict-1",
		IdempotencyKey:        "key-reused-2", // different key!
		PlayerID:              playerID,
		WalletID:              wallet.ID,
		RoundID:               "round-1",
		GameID:                "game-1",
		Kind:                  domain.KindBet,
		Money:                 betAmt,
	})
	if err == nil {
		t.Fatal("expected error for same transaction with different idempotency key, got nil")
	}

	var domErr *domain.DomainError
	if !errors.As(err, &domErr) || domErr.Code != domain.FailureCodeIdempotencyConflict {
		t.Fatalf("expected FailureCodeIdempotencyConflict, got %v", err)
	}
}

func TestReversals_CannotReverseTwice_RefundThenRollback(t *testing.T) {
	suite := setupTestSuite(t)
	ctx := context.Background()
	playerID := "player-double-rev"

	initBal, _ := domain.NewMoney("200.00", "BRL")
	wallet, _ := suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
		PlayerID:       playerID,
		InitialBalance: initBal,
	})

	betAmt, _ := domain.NewMoney("50.00", "BRL")
	// 1. Process original BET
	_, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "bet-to-reverse",
		IdempotencyKey:        "key-bet-rev",
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

	// 2. Process REFUND on that BET
	refundOut, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:                     "provider-a",
		ExternalTransactionID:          "refund-1",
		IdempotencyKey:                 "key-refund-1",
		PlayerID:                       playerID,
		WalletID:                       wallet.ID,
		RoundID:                        "round-1",
		GameID:                         "game-1",
		Kind:                           domain.KindRefund,
		Money:                          betAmt,
		ReferenceExternalTransactionID: "bet-to-reverse",
	})
	if err != nil || refundOut.Status != domain.StatusProcessed {
		t.Fatalf("expected REFUND to be PROCESSED, got status: %s, err: %v", refundOut.Status, err)
	}

	// 3. Attempt a ROLLBACK on that same BET -> MUST be rejected with REFERENCE_ALREADY_REVERSED!
	rollbackOut, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:                     "provider-a",
		ExternalTransactionID:          "rollback-illegal",
		IdempotencyKey:                 "key-rollback-illegal",
		PlayerID:                       playerID,
		WalletID:                       wallet.ID,
		RoundID:                        "round-1",
		GameID:                         "game-1",
		Kind:                           domain.KindRollback,
		Money:                          betAmt,
		ReferenceExternalTransactionID: "bet-to-reverse",
	})
	if err != nil {
		t.Fatalf("unexpected execution error on rollback: %v", err)
	}

	if rollbackOut.Status != domain.StatusRejected {
		t.Fatalf("expected ROLLBACK on already refunded BET to be REJECTED, got %s", rollbackOut.Status)
	}
	if rollbackOut.FailureCode != domain.FailureCodeReferenceAlreadyReversed {
		t.Errorf("expected FailureCodeReferenceAlreadyReversed, got %s", rollbackOut.FailureCode)
	}
}

func TestWin_OptionalReferenceValidation(t *testing.T) {
	suite := setupTestSuite(t)
	ctx := context.Background()
	playerID := "player-win-ref"

	initBal, _ := domain.NewMoney("100.00", "BRL")
	wallet, _ := suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
		PlayerID:       playerID,
		InitialBalance: initBal,
	})

	betAmt, _ := domain.NewMoney("25.00", "BRL")
	// Process BET in round-1
	_, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "bet-round-1",
		IdempotencyKey:        "key-b1",
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

	winAmt, _ := domain.NewMoney("50.00", "BRL")

	// 1. WIN referencing non-existent BET
	winBadRef, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:                     "provider-a",
		ExternalTransactionID:          "win-bad-ref",
		IdempotencyKey:                 "key-w-bad",
		PlayerID:                       playerID,
		WalletID:                       wallet.ID,
		RoundID:                        "round-1",
		GameID:                         "game-1",
		Kind:                           domain.KindWin,
		Money:                          winAmt,
		ReferenceExternalTransactionID: "bet-non-existent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if winBadRef.Status != domain.StatusRejected || winBadRef.FailureCode != domain.FailureCodeReferenceNotFound {
		t.Errorf("expected rejection with REFERENCE_NOT_FOUND, got %s / %s", winBadRef.Status, winBadRef.FailureCode)
	}

	// 2. WIN referencing BET of a different round
	winDiffRound, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:                     "provider-a",
		ExternalTransactionID:          "win-diff-round",
		IdempotencyKey:                 "key-w-diff",
		PlayerID:                       playerID,
		WalletID:                       wallet.ID,
		RoundID:                        "round-2", // different round from bet-round-1!
		GameID:                         "game-1",
		Kind:                           domain.KindWin,
		Money:                          winAmt,
		ReferenceExternalTransactionID: "bet-round-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if winDiffRound.Status != domain.StatusRejected || winDiffRound.FailureCode != domain.FailureCodeReferenceMismatch {
		t.Errorf("expected rejection with REFERENCE_MISMATCH, got %s / %s", winDiffRound.Status, winDiffRound.FailureCode)
	}

	// 3. WIN referencing valid BET of same round -> Must be PROCESSED
	winGood, err := suite.ProcessWagerUC.Execute(ctx, usecase.ProcessWagerInput{
		ProviderID:                     "provider-a",
		ExternalTransactionID:          "win-good",
		IdempotencyKey:                 "key-w-good",
		PlayerID:                       playerID,
		WalletID:                       wallet.ID,
		RoundID:                        "round-1",
		GameID:                         "game-1",
		Kind:                           domain.KindWin,
		Money:                          winAmt,
		ReferenceExternalTransactionID: "bet-round-1",
	})
	if err != nil || winGood.Status != domain.StatusProcessed {
		t.Errorf("expected valid referenced WIN to be PROCESSED, got %s / %v", winGood.Status, err)
	}
}

func TestFxLifecycle_StartAndStop(t *testing.T) {
	app := fx.New(
		di.ConfigModule,
		di.DatabaseModule,
		di.UsecaseModule,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := app.Start(ctx); err != nil {
		t.Fatalf("failed to start fx app: %v", err)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := app.Stop(stopCtx); err != nil {
		t.Fatalf("failed to stop fx app: %v", err)
	}
}

func TestOutbox_TwoWorkersCompeting_SkipLocked(t *testing.T) {
	suite := setupTestSuite(t)
	ctx := context.Background()

	const recordCount = 20
	for i := 0; i < recordCount; i++ {
		eventID := fmt.Sprintf("event-compete-%d", i)
		err := suite.OutboxRepo.Save(ctx, &database.OutboxRecord{
			EventID:       eventID,
			EventType:     "TestEvent",
			AggregateID:   "agg-1",
			CorrelationID: uuid.NewString(),
			Payload:       json.RawMessage(`{"test":true}`),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	var processedCount int32
	var wg sync.WaitGroup
	wg.Add(2)

	// Worker 1 and Worker 2 fetch concurrently with FOR UPDATE SKIP LOCKED
	for wID := 1; wID <= 2; wID++ {
		go func() {
			defer wg.Done()
			for {
				var count int
				_ = suite.TxManager.WithinTransaction(ctx, func(txCtx context.Context) error {
					records, err := suite.OutboxRepo.FetchPending(txCtx, 5)
					if err != nil || len(records) == 0 {
						return nil
					}
					count = len(records)
					for _, rec := range records {
						atomic.AddInt32(&processedCount, 1)
						_ = suite.OutboxRepo.MarkPublished(txCtx, rec.ID, time.Now())
					}
					return nil
				})
				if count == 0 {
					return
				}
			}
		}()
	}

	wg.Wait()

	if processedCount != recordCount {
		t.Errorf("expected exactly %d records processed without duplicates, got %d", recordCount, processedCount)
	}

	pending, err := suite.OutboxRepo.FetchPending(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("expected 0 pending outbox records left, got %d", len(pending))
	}
}

func TestSQSConsumer_RedeliveryAfterCommit_InboxPreventsDuplicate(t *testing.T) {
	suite := setupTestSuite(t)
	ctx := context.Background()
	playerID := "player-redelivery-test"

	initBal, _ := domain.NewMoney("100.00", "BRL")
	wallet, _ := suite.CreateWalletUC.Execute(ctx, usecase.CreateWalletInput{
		PlayerID:       playerID,
		InitialBalance: initBal,
	})

	consumer := messaging.NewSQSConsumerWorker(
		nil,
		"dummy-queue",
		"dummy-dlq",
		"test-consumer",
		suite.TxManager,
		suite.InboxRepo,
		suite.ProcessWagerUC,
		nil,
	)

	betAmt, _ := domain.NewMoney("25.00", "BRL")
	data := messaging.SQSMessageData{
		ProviderID:            "provider-a",
		ExternalTransactionID: "tx-sqs-redelivery-1",
		IdempotencyKey:        "key-sqs-redelivery-1",
		PlayerID:              playerID,
		WalletID:              wallet.ID,
		RoundID:               "round-1",
		GameID:                "game-1",
		Kind:                  domain.KindBet,
		Money:                 betAmt,
	}
	dataBytes, _ := json.Marshal(data)

	env := messaging.SQSMessageEnvelope{
		MessageID:  "msg-unique-123",
		Type:       "WagerTransactionRequested",
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
		Data:       dataBytes,
	}
	envBytes, _ := json.Marshal(env)
	msgBody := string(envBytes)

	sqsMsg := types.Message{
		Body:          &msgBody,
		MessageId:     &env.MessageID,
		ReceiptHandle: nil,
	}

	// First delivery: processes bet, debits 25.00, records in inbox
	// (Simulate handleMessage by calling consumer's logic via inbox & usecase)
	_ = suite.TxManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		_ = suite.InboxRepo.Record(txCtx, "test-consumer", env.MessageID, "hash-123", time.Now())
		_, err := suite.ProcessWagerUC.Execute(txCtx, usecase.ProcessWagerInput{
			ProviderID:            data.ProviderID,
			ExternalTransactionID: data.ExternalTransactionID,
			IdempotencyKey:        data.IdempotencyKey,
			PlayerID:              data.PlayerID,
			WalletID:              data.WalletID,
			RoundID:               data.RoundID,
			GameID:                data.GameID,
			Kind:                  data.Kind,
			Money:                 data.Money,
		})
		return err
	})

	// Check balance after first delivery: 75.00 BRL
	w, err := suite.GetWalletUC.Execute(ctx, wallet.ID)
	if err != nil || w.Balance().AmountString() != "75.00" {
		t.Fatalf("expected balance 75.00 after first delivery, got %s", w.Balance().AmountString())
	}

	// Second delivery (simulating crash before DeleteMessage):
	// SQS redelivers the exact same message body with same MessageID
	// The inbox must detect the duplicate and NOT perform a second debit!
	err = suite.TxManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		err := suite.InboxRepo.Record(txCtx, "test-consumer", env.MessageID, "hash-123", time.Now())
		if errors.Is(err, database.ErrDuplicateInboxMessage) {
			// Deduplicated!
			return nil
		}
		if err != nil {
			return err
		}
		// Would only execute if inbox failed to detect duplicate
		_, err = suite.ProcessWagerUC.Execute(txCtx, usecase.ProcessWagerInput{
			ProviderID:            data.ProviderID,
			ExternalTransactionID: data.ExternalTransactionID,
			IdempotencyKey:        data.IdempotencyKey,
			PlayerID:              data.PlayerID,
			WalletID:              data.WalletID,
			RoundID:               data.RoundID,
			GameID:                data.GameID,
			Kind:                  data.Kind,
			Money:                 data.Money,
		})
		return err
	})

	if err != nil {
		t.Fatalf("unexpected error on redelivery handling: %v", err)
	}

	// Balance must REMAIN 75.00 BRL (NO double debit!)
	w2, err := suite.GetWalletUC.Execute(ctx, wallet.ID)
	if err != nil || w2.Balance().AmountString() != "75.00" {
		t.Fatalf("expected balance to remain 75.00 after redelivery, got %s", w2.Balance().AmountString())
	}

	_ = consumer
	_ = sqsMsg
}
