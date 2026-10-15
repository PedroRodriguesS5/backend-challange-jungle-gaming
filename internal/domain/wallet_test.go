package domain_test

import (
	"errors"
	"testing"
	"time"

	"backend-challenge-go-jungle-gaming/internal/domain"
)

func TestNewWallet(t *testing.T) {
	now := time.Now().UTC()
	bal100, _ := domain.NewMoney("100.00", "BRL")
	bal0, _ := domain.NewMoney("0.00", "BRL")
	balNeg, _ := domain.NewMoney("-10.00", "BRL")

	tests := []struct {
		name           string
		id             string
		playerID       string
		initialBalance domain.Money
		expectedErr    error
	}{
		{
			name:           "valid positive balance",
			id:             "wallet-1",
			playerID:       "player-1",
			initialBalance: bal100,
			expectedErr:    nil,
		},
		{
			name:           "valid zero balance",
			id:             "wallet-2",
			playerID:       "player-2",
			initialBalance: bal0,
			expectedErr:    nil,
		},
		{
			name:           "negative initial balance rejected",
			id:             "wallet-3",
			playerID:       "player-3",
			initialBalance: balNeg,
			expectedErr:    domain.ErrInvalidInitialBalance,
		},
		{
			name:           "empty wallet id",
			id:             "",
			playerID:       "player-1",
			initialBalance: bal100,
			expectedErr:    domain.ErrEmptyWalletID,
		},
		{
			name:           "empty player id",
			id:             "wallet-1",
			playerID:       "",
			initialBalance: bal100,
			expectedErr:    domain.ErrEmptyPlayerID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := domain.NewWallet(tt.id, tt.playerID, tt.initialBalance, now)
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
			if w.ID() != tt.id {
				t.Errorf("expected id %s, got %s", tt.id, w.ID())
			}
			if w.PlayerID() != tt.playerID {
				t.Errorf("expected player id %s, got %s", tt.playerID, w.PlayerID())
			}
			if !w.Balance().Equal(tt.initialBalance) {
				t.Errorf("expected balance %v, got %v", tt.initialBalance, w.Balance())
			}
			if w.Version() != 1 {
				t.Errorf("expected version 1, got %d", w.Version())
			}
		})
	}
}

func TestRehydrateWallet(t *testing.T) {
	now := time.Now().UTC()
	bal100, _ := domain.NewMoney("100.00", "BRL")
	balNeg, _ := domain.NewMoney("-50.00", "BRL")

	tests := []struct {
		name        string
		id          string
		playerID    string
		balance     domain.Money
		version     int64
		expectedErr error
	}{
		{
			name:        "valid rehydration",
			id:          "wallet-1",
			playerID:    "player-1",
			balance:     bal100,
			version:     5,
			expectedErr: nil,
		},
		{
			name:        "invalid version 0",
			id:          "wallet-1",
			playerID:    "player-1",
			balance:     bal100,
			version:     0,
			expectedErr: domain.ErrInvalidVersion,
		},
		{
			name:        "negative balance rejected",
			id:          "wallet-1",
			playerID:    "player-1",
			balance:     balNeg,
			version:     2,
			expectedErr: domain.ErrNegativeBalance,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := domain.RehydrateWallet(tt.id, tt.playerID, tt.balance, tt.version, now, now)
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
			if w.Version() != tt.version {
				t.Errorf("expected version %d, got %d", tt.version, w.Version())
			}
		})
	}
}

func TestWallet_Debit(t *testing.T) {
	now := time.Now().UTC()
	bal100, _ := domain.NewMoney("100.00", "BRL")
	amt30, _ := domain.NewMoney("30.00", "BRL")
	amt150, _ := domain.NewMoney("150.00", "BRL")
	amt0, _ := domain.NewMoney("0.00", "BRL")
	amtUSD, _ := domain.NewMoney("30.00", "USD")

	t.Run("successful debit", func(t *testing.T) {
		w, err := domain.NewWallet("wallet-1", "player-1", bal100, now)
		if err != nil {
			t.Fatal(err)
		}

		entry, err := w.Debit("tx-1", "entry-1", amt30, now)
		if err != nil {
			t.Fatalf("unexpected debit error: %v", err)
		}

		if w.Balance().AmountString() != "70.00" {
			t.Errorf("expected balance 70.00, got %s", w.Balance().AmountString())
		}
		if w.Version() != 2 {
			t.Errorf("expected version 2, got %d", w.Version())
		}
		if entry.Direction() != domain.DirectionDebit {
			t.Errorf("expected DEBIT, got %s", entry.Direction())
		}
		if entry.BalanceAfter().AmountString() != "70.00" {
			t.Errorf("expected balanceAfter 70.00, got %s", entry.BalanceAfter().AmountString())
		}
	})

	t.Run("insufficient funds", func(t *testing.T) {
		w, _ := domain.NewWallet("wallet-1", "player-1", bal100, now)
		_, err := w.Debit("tx-2", "entry-2", amt150, now)
		if !errors.Is(err, domain.ErrInsufficientFunds) {
			t.Errorf("expected ErrInsufficientFunds, got %v", err)
		}
		if w.Balance().AmountString() != "100.00" {
			t.Errorf("balance should remain 100.00, got %s", w.Balance().AmountString())
		}
		if w.Version() != 1 {
			t.Errorf("version should remain 1, got %d", w.Version())
		}
	})

	t.Run("currency mismatch", func(t *testing.T) {
		w, _ := domain.NewWallet("wallet-1", "player-1", bal100, now)
		_, err := w.Debit("tx-3", "entry-3", amtUSD, now)
		if !errors.Is(err, domain.ErrCurrencyMismatch) {
			t.Errorf("expected ErrCurrencyMismatch, got %v", err)
		}
	})

	t.Run("zero or negative amount", func(t *testing.T) {
		w, _ := domain.NewWallet("wallet-1", "player-1", bal100, now)
		_, err := w.Debit("tx-4", "entry-4", amt0, now)
		if !errors.Is(err, domain.ErrInvalidAmount) {
			t.Errorf("expected ErrInvalidAmount, got %v", err)
		}
	})
}

func TestWallet_Credit(t *testing.T) {
	now := time.Now().UTC()
	bal100, _ := domain.NewMoney("100.00", "BRL")
	amt50, _ := domain.NewMoney("50.00", "BRL")
	amtUSD, _ := domain.NewMoney("50.00", "USD")
	amt0, _ := domain.NewMoney("0.00", "BRL")

	t.Run("successful credit", func(t *testing.T) {
		w, err := domain.NewWallet("wallet-1", "player-1", bal100, now)
		if err != nil {
			t.Fatal(err)
		}

		entry, err := w.Credit("tx-1", "entry-1", amt50, now)
		if err != nil {
			t.Fatalf("unexpected credit error: %v", err)
		}

		if w.Balance().AmountString() != "150.00" {
			t.Errorf("expected balance 150.00, got %s", w.Balance().AmountString())
		}
		if w.Version() != 2 {
			t.Errorf("expected version 2, got %d", w.Version())
		}
		if entry.Direction() != domain.DirectionCredit {
			t.Errorf("expected CREDIT, got %s", entry.Direction())
		}
		if entry.BalanceAfter().AmountString() != "150.00" {
			t.Errorf("expected balanceAfter 150.00, got %s", entry.BalanceAfter().AmountString())
		}
	})

	t.Run("currency mismatch", func(t *testing.T) {
		w, _ := domain.NewWallet("wallet-1", "player-1", bal100, now)
		_, err := w.Credit("tx-2", "entry-2", amtUSD, now)
		if !errors.Is(err, domain.ErrCurrencyMismatch) {
			t.Errorf("expected ErrCurrencyMismatch, got %v", err)
		}
	})

	t.Run("zero amount", func(t *testing.T) {
		w, _ := domain.NewWallet("wallet-1", "player-1", bal100, now)
		_, err := w.Credit("tx-3", "entry-3", amt0, now)
		if !errors.Is(err, domain.ErrInvalidAmount) {
			t.Errorf("expected ErrInvalidAmount, got %v", err)
		}
	})
}

func TestWallet_CreateOpeningLedgerEntry(t *testing.T) {
	now := time.Now().UTC()
	bal100, _ := domain.NewMoney("100.00", "BRL")
	bal0, _ := domain.NewMoney("0.00", "BRL")

	t.Run("positive initial balance creates opening ledger entry", func(t *testing.T) {
		w, _ := domain.NewWallet("wallet-1", "player-1", bal100, now)
		entry, err := w.CreateOpeningLedgerEntry("tx-open", "entry-open", now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if entry.Direction() != domain.DirectionCredit {
			t.Errorf("expected direction CREDIT, got %s", entry.Direction())
		}
		if entry.BalanceBefore().AmountString() != "0.00" {
			t.Errorf("expected balanceBefore 0.00, got %s", entry.BalanceBefore().AmountString())
		}
		if entry.BalanceAfter().AmountString() != "100.00" {
			t.Errorf("expected balanceAfter 100.00, got %s", entry.BalanceAfter().AmountString())
		}
	})

	t.Run("zero initial balance rejects opening ledger entry", func(t *testing.T) {
		w, _ := domain.NewWallet("wallet-2", "player-2", bal0, now)
		_, err := w.CreateOpeningLedgerEntry("tx-open", "entry-open", now)
		if !errors.Is(err, domain.ErrInvalidAmount) {
			t.Errorf("expected ErrInvalidAmount, got %v", err)
		}
	})
}
