package domain_test

import (
	"errors"
	"testing"
	"time"

	"backend-challenge-go-jungle-gaming/internal/domain"
)

func TestNewWagerTransaction(t *testing.T) {
	now := time.Now().UTC()
	amt25, _ := domain.NewMoney("25.00", "BRL")
	amt0, _ := domain.NewMoney("0.00", "BRL")
	amtNeg, _ := domain.NewMoney("-25.00", "BRL")

	tests := []struct {
		name        string
		id          string
		externalID  string
		providerID  string
		idempotency string
		payloadHash string
		walletID    string
		playerID    string
		roundID     string
		gameID      string
		kind        domain.WagerKind
		amount      domain.Money
		referenceID string
		expectedErr error
	}{
		{
			name:        "valid BET",
			id:          "tx-1",
			externalID:  "ext-1",
			providerID:  "provider-a",
			idempotency: "key-1",
			payloadHash: "hash-1",
			walletID:    "wallet-1",
			playerID:    "player-1",
			roundID:     "round-1",
			gameID:      "game-1",
			kind:        domain.KindBet,
			amount:      amt25,
			referenceID: "",
			expectedErr: nil,
		},
		{
			name:        "valid WIN",
			id:          "tx-2",
			externalID:  "ext-2",
			providerID:  "provider-a",
			idempotency: "key-2",
			payloadHash: "hash-2",
			walletID:    "wallet-1",
			playerID:    "player-1",
			roundID:     "round-1",
			gameID:      "game-1",
			kind:        domain.KindWin,
			amount:      amt25,
			referenceID: "",
			expectedErr: nil,
		},
		{
			name:        "valid LOSS with zero amount",
			id:          "tx-3",
			externalID:  "ext-3",
			providerID:  "provider-a",
			idempotency: "key-3",
			payloadHash: "hash-3",
			walletID:    "wallet-1",
			playerID:    "player-1",
			roundID:     "round-1",
			gameID:      "game-1",
			kind:        domain.KindLoss,
			amount:      amt0,
			referenceID: "",
			expectedErr: nil,
		},
		{
			name:        "invalid LOSS with non-zero amount",
			id:          "tx-4",
			externalID:  "ext-4",
			providerID:  "provider-a",
			idempotency: "key-4",
			payloadHash: "hash-4",
			walletID:    "wallet-1",
			playerID:    "player-1",
			roundID:     "round-1",
			gameID:      "game-1",
			kind:        domain.KindLoss,
			amount:      amt25,
			referenceID: "",
			expectedErr: domain.ErrInvalidLossAmount,
		},
		{
			name:        "valid REFUND with reference",
			id:          "tx-5",
			externalID:  "ext-5",
			providerID:  "provider-a",
			idempotency: "key-5",
			payloadHash: "hash-5",
			walletID:    "wallet-1",
			playerID:    "player-1",
			roundID:     "round-1",
			gameID:      "game-1",
			kind:        domain.KindRefund,
			amount:      amt25,
			referenceID: "ext-1",
			expectedErr: nil,
		},
		{
			name:        "REFUND missing reference",
			id:          "tx-6",
			externalID:  "ext-6",
			providerID:  "provider-a",
			idempotency: "key-6",
			payloadHash: "hash-6",
			walletID:    "wallet-1",
			playerID:    "player-1",
			roundID:     "round-1",
			gameID:      "game-1",
			kind:        domain.KindRefund,
			amount:      amt25,
			referenceID: "",
			expectedErr: domain.ErrMissingReferenceID,
		},
		{
			name:        "valid ROLLBACK with reference",
			id:          "tx-7",
			externalID:  "ext-7",
			providerID:  "provider-a",
			idempotency: "key-7",
			payloadHash: "hash-7",
			walletID:    "wallet-1",
			playerID:    "player-1",
			roundID:     "round-1",
			gameID:      "game-1",
			kind:        domain.KindRollback,
			amount:      amt25,
			referenceID: "ext-1",
			expectedErr: nil,
		},
		{
			name:        "ROLLBACK missing reference",
			id:          "tx-8",
			externalID:  "ext-8",
			providerID:  "provider-a",
			idempotency: "key-8",
			payloadHash: "hash-8",
			walletID:    "wallet-1",
			playerID:    "player-1",
			roundID:     "round-1",
			gameID:      "game-1",
			kind:        domain.KindRollback,
			amount:      amt25,
			referenceID: "",
			expectedErr: domain.ErrMissingReferenceID,
		},
		{
			name:        "external OPENING rejected",
			id:          "tx-9",
			externalID:  "ext-9",
			providerID:  "provider-a",
			idempotency: "key-9",
			payloadHash: "hash-9",
			walletID:    "wallet-1",
			playerID:    "player-1",
			roundID:     "round-1",
			gameID:      "game-1",
			kind:        domain.KindOpening,
			amount:      amt25,
			referenceID: "",
			expectedErr: domain.ErrInternalTransactionForbidden,
		},
		{
			name:        "BET with zero amount rejected",
			id:          "tx-10",
			externalID:  "ext-10",
			providerID:  "provider-a",
			idempotency: "key-10",
			payloadHash: "hash-10",
			walletID:    "wallet-1",
			playerID:    "player-1",
			roundID:     "round-1",
			gameID:      "game-1",
			kind:        domain.KindBet,
			amount:      amt0,
			referenceID: "",
			expectedErr: domain.ErrInvalidAmount,
		},
		{
			name:        "BET with negative amount rejected",
			id:          "tx-11",
			externalID:  "ext-11",
			providerID:  "provider-a",
			idempotency: "key-11",
			payloadHash: "hash-11",
			walletID:    "wallet-1",
			playerID:    "player-1",
			roundID:     "round-1",
			gameID:      "game-1",
			kind:        domain.KindBet,
			amount:      amtNeg,
			referenceID: "",
			expectedErr: domain.ErrInvalidAmount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := domain.NewWagerTransaction(
				tt.id,
				tt.externalID,
				tt.providerID,
				tt.idempotency,
				tt.payloadHash,
				tt.walletID,
				tt.playerID,
				tt.roundID,
				tt.gameID,
				tt.kind,
				tt.amount,
				tt.referenceID,
				now,
			)

			if tt.expectedErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.expectedErr)
				}
				if !errors.Is(err, tt.expectedErr) {
					t.Fatalf("expected error %v, got %v", tt.expectedErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tx.Status() != domain.StatusPending {
				t.Errorf("expected initial status PENDING, got %s", tx.Status())
			}
		})
	}
}

func TestInternalOpeningTransaction(t *testing.T) {
	now := time.Now().UTC()
	amt100, _ := domain.NewMoney("100.00", "BRL")
	amt0, _ := domain.NewMoney("0.00", "BRL")

	t.Run("valid internal opening", func(t *testing.T) {
		tx, err := domain.NewInternalOpeningTransaction("open-1", "wallet-1", "player-1", amt100, now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tx.Kind() != domain.KindOpening {
			t.Errorf("expected kind OPENING, got %s", tx.Kind())
		}
		if tx.Status() != domain.StatusProcessed {
			t.Errorf("expected status PROCESSED, got %s", tx.Status())
		}
		if !tx.IsTerminal() {
			t.Error("expected OPENING to be terminal")
		}
	})

	t.Run("zero amount rejected", func(t *testing.T) {
		_, err := domain.NewInternalOpeningTransaction("open-2", "wallet-1", "player-1", amt0, now)
		if !errors.Is(err, domain.ErrInvalidAmount) {
			t.Errorf("expected ErrInvalidAmount, got %v", err)
		}
	})
}

func TestWagerTransaction_StateTransitions(t *testing.T) {
	now := time.Now().UTC()
	amt25, _ := domain.NewMoney("25.00", "BRL")

	createPending := func(t *testing.T) *domain.WagerTransaction {
		t.Helper()
		tx, err := domain.NewWagerTransaction(
			"tx-1", "ext-1", "provider-a", "key-1", "hash-1",
			"wallet-1", "player-1", "round-1", "game-1",
			domain.KindBet, amt25, "", now,
		)
		if err != nil {
			t.Fatal(err)
		}
		return tx
	}

	t.Run("transition to processed", func(t *testing.T) {
		tx := createPending(t)
		if err := tx.TransitionToProcessed(now); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tx.Status() != domain.StatusProcessed {
			t.Errorf("expected PROCESSED, got %s", tx.Status())
		}
		if !tx.IsTerminal() {
			t.Error("expected terminal state")
		}
		if err := tx.TransitionToRejected("TEST", now); !errors.Is(err, domain.ErrTerminalState) {
			t.Errorf("expected ErrTerminalState when mutating PROCESSED, got %v", err)
		}
	})

	t.Run("transition to pending reference then processed", func(t *testing.T) {
		tx := createPending(t)
		if err := tx.TransitionToPendingReference(now); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tx.Status() != domain.StatusPendingReference {
			t.Errorf("expected PENDING_REFERENCE, got %s", tx.Status())
		}
		if tx.IsTerminal() {
			t.Error("PENDING_REFERENCE should not be terminal")
		}
		if err := tx.TransitionToProcessed(now); err != nil {
			t.Fatalf("unexpected transition to processed: %v", err)
		}
		if tx.Status() != domain.StatusProcessed {
			t.Errorf("expected PROCESSED, got %s", tx.Status())
		}
	})

	t.Run("transition to rejected", func(t *testing.T) {
		tx := createPending(t)
		if err := tx.TransitionToRejected(domain.FailureCodeInsufficientFunds, now); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tx.Status() != domain.StatusRejected {
			t.Errorf("expected REJECTED, got %s", tx.Status())
		}
		if tx.FailureCode() != domain.FailureCodeInsufficientFunds {
			t.Errorf("expected failure code %s, got %s", domain.FailureCodeInsufficientFunds, tx.FailureCode())
		}
		if !tx.IsTerminal() {
			t.Error("expected terminal state")
		}
		if err := tx.TransitionToProcessed(now); !errors.Is(err, domain.ErrTerminalState) {
			t.Errorf("expected ErrTerminalState when mutating REJECTED, got %v", err)
		}
	})

	t.Run("transition to failed", func(t *testing.T) {
		tx := createPending(t)
		if err := tx.TransitionToFailed("INFRA_ERROR", now); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tx.Status() != domain.StatusFailed {
			t.Errorf("expected FAILED, got %s", tx.Status())
		}
		if !tx.IsTerminal() {
			t.Error("expected terminal state")
		}
		if err := tx.TransitionToProcessed(now); !errors.Is(err, domain.ErrTerminalState) {
			t.Errorf("expected ErrTerminalState when mutating FAILED, got %v", err)
		}
	})
}

func TestWagerTransaction_ValidateReference(t *testing.T) {
	now := time.Now().UTC()
	amt25BRL, _ := domain.NewMoney("25.00", "BRL")
	amt30BRL, _ := domain.NewMoney("30.00", "BRL")
	amt25USD, _ := domain.NewMoney("25.00", "USD")

	original, err := domain.NewWagerTransaction(
		"orig-1", "ext-orig-1", "provider-a", "key-orig", "hash-orig",
		"wallet-1", "player-1", "round-1", "game-1",
		domain.KindBet, amt25BRL, "", now,
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("nil reference", func(t *testing.T) {
		refund, _ := domain.NewWagerTransaction(
			"ref-1", "ext-ref-1", "provider-a", "key-ref", "hash-ref",
			"wallet-1", "player-1", "round-1", "game-1",
			domain.KindRefund, amt25BRL, "ext-orig-1", now,
		)
		err := refund.ValidateReference(nil)
		if !errors.Is(err, domain.ErrReferenceNotFound) {
			t.Errorf("expected ErrReferenceNotFound, got %v", err)
		}
	})

	t.Run("unprocessed reference", func(t *testing.T) {
		refund, _ := domain.NewWagerTransaction(
			"ref-1", "ext-ref-1", "provider-a", "key-ref", "hash-ref",
			"wallet-1", "player-1", "round-1", "game-1",
			domain.KindRefund, amt25BRL, "ext-orig-1", now,
		)
		err := refund.ValidateReference(original)
		if !errors.Is(err, domain.ErrReferenceMismatch) {
			t.Errorf("expected ErrReferenceMismatch for non-processed reference, got %v", err)
		}
	})

	if err := original.TransitionToProcessed(now); err != nil {
		t.Fatal(err)
	}

	t.Run("valid reference match", func(t *testing.T) {
		refund, _ := domain.NewWagerTransaction(
			"ref-1", "ext-ref-1", "provider-a", "key-ref", "hash-ref",
			"wallet-1", "player-1", "round-1", "game-1",
			domain.KindRefund, amt25BRL, "ext-orig-1", now,
		)
		if err := refund.ValidateReference(original); err != nil {
			t.Fatalf("unexpected error validating reference: %v", err)
		}
	})

	t.Run("provider mismatch", func(t *testing.T) {
		refund, _ := domain.NewWagerTransaction(
			"ref-1", "ext-ref-1", "provider-b", "key-ref", "hash-ref",
			"wallet-1", "player-1", "round-1", "game-1",
			domain.KindRefund, amt25BRL, "ext-orig-1", now,
		)
		if err := refund.ValidateReference(original); !errors.Is(err, domain.ErrReferenceMismatch) {
			t.Errorf("expected ErrReferenceMismatch, got %v", err)
		}
	})

	t.Run("player mismatch", func(t *testing.T) {
		refund, _ := domain.NewWagerTransaction(
			"ref-1", "ext-ref-1", "provider-a", "key-ref", "hash-ref",
			"wallet-1", "player-2", "round-1", "game-1",
			domain.KindRefund, amt25BRL, "ext-orig-1", now,
		)
		if err := refund.ValidateReference(original); !errors.Is(err, domain.ErrReferenceMismatch) {
			t.Errorf("expected ErrReferenceMismatch, got %v", err)
		}
	})

	t.Run("wallet mismatch", func(t *testing.T) {
		refund, _ := domain.NewWagerTransaction(
			"ref-1", "ext-ref-1", "provider-a", "key-ref", "hash-ref",
			"wallet-2", "player-1", "round-1", "game-1",
			domain.KindRefund, amt25BRL, "ext-orig-1", now,
		)
		if err := refund.ValidateReference(original); !errors.Is(err, domain.ErrReferenceMismatch) {
			t.Errorf("expected ErrReferenceMismatch, got %v", err)
		}
	})

	t.Run("round mismatch", func(t *testing.T) {
		refund, _ := domain.NewWagerTransaction(
			"ref-1", "ext-ref-1", "provider-a", "key-ref", "hash-ref",
			"wallet-1", "player-1", "round-2", "game-1",
			domain.KindRefund, amt25BRL, "ext-orig-1", now,
		)
		if err := refund.ValidateReference(original); !errors.Is(err, domain.ErrReferenceMismatch) {
			t.Errorf("expected ErrReferenceMismatch, got %v", err)
		}
	})

	t.Run("amount mismatch", func(t *testing.T) {
		refund, _ := domain.NewWagerTransaction(
			"ref-1", "ext-ref-1", "provider-a", "key-ref", "hash-ref",
			"wallet-1", "player-1", "round-1", "game-1",
			domain.KindRefund, amt30BRL, "ext-orig-1", now,
		)
		if err := refund.ValidateReference(original); !errors.Is(err, domain.ErrReferenceMismatch) {
			t.Errorf("expected ErrReferenceMismatch, got %v", err)
		}
	})

	t.Run("currency mismatch", func(t *testing.T) {
		refund, _ := domain.NewWagerTransaction(
			"ref-1", "ext-ref-1", "provider-a", "key-ref", "hash-ref",
			"wallet-1", "player-1", "round-1", "game-1",
			domain.KindRefund, amt25USD, "ext-orig-1", now,
		)
		if err := refund.ValidateReference(original); !errors.Is(err, domain.ErrCurrencyMismatch) {
			t.Errorf("expected ErrCurrencyMismatch, got %v", err)
		}
	})
}
