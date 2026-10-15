package domain_test

import (
	"errors"
	"testing"
	"time"

	"backend-challenge-go-jungle-gaming/internal/domain"
)

func TestNewWalletLedgerEntry(t *testing.T) {
	now := time.Now().UTC()
	amt10, _ := domain.NewMoney("10.00", "BRL")
	amt0, _ := domain.NewMoney("0.00", "BRL")
	amtNeg, _ := domain.NewMoney("-10.00", "BRL")
	bal100, _ := domain.NewMoney("100.00", "BRL")
	bal110, _ := domain.NewMoney("110.00", "BRL")
	bal90, _ := domain.NewMoney("90.00", "BRL")
	bal105, _ := domain.NewMoney("105.00", "BRL")
	balUSD, _ := domain.NewMoney("100.00", "USD")

	tests := []struct {
		name          string
		id            string
		walletID      string
		transactionID string
		direction     domain.LedgerDirection
		amount        domain.Money
		balanceBefore domain.Money
		balanceAfter  domain.Money
		expectedErr   error
	}{
		{
			name:          "valid credit",
			id:            "entry-1",
			walletID:      "wallet-1",
			transactionID: "tx-1",
			direction:     domain.DirectionCredit,
			amount:        amt10,
			balanceBefore: bal100,
			balanceAfter:  bal110,
			expectedErr:   nil,
		},
		{
			name:          "valid debit",
			id:            "entry-2",
			walletID:      "wallet-1",
			transactionID: "tx-2",
			direction:     domain.DirectionDebit,
			amount:        amt10,
			balanceBefore: bal100,
			balanceAfter:  bal90,
			expectedErr:   nil,
		},
		{
			name:          "credit math mismatch",
			id:            "entry-3",
			walletID:      "wallet-1",
			transactionID: "tx-3",
			direction:     domain.DirectionCredit,
			amount:        amt10,
			balanceBefore: bal100,
			balanceAfter:  bal105,
			expectedErr:   domain.ErrInvalidLedgerMath,
		},
		{
			name:          "debit math mismatch",
			id:            "entry-4",
			walletID:      "wallet-1",
			transactionID: "tx-4",
			direction:     domain.DirectionDebit,
			amount:        amt10,
			balanceBefore: bal100,
			balanceAfter:  bal105,
			expectedErr:   domain.ErrInvalidLedgerMath,
		},
		{
			name:          "debit resulting in negative balance",
			id:            "entry-5",
			walletID:      "wallet-1",
			transactionID: "tx-5",
			direction:     domain.DirectionDebit,
			amount:        bal110,
			balanceBefore: bal100,
			balanceAfter:  amtNeg,
			expectedErr:   domain.ErrNegativeBalance,
		},
		{
			name:          "currency mismatch in balances",
			id:            "entry-6",
			walletID:      "wallet-1",
			transactionID: "tx-6",
			direction:     domain.DirectionCredit,
			amount:        amt10,
			balanceBefore: balUSD,
			balanceAfter:  bal110,
			expectedErr:   domain.ErrCurrencyMismatch,
		},
		{
			name:          "zero amount rejected",
			id:            "entry-7",
			walletID:      "wallet-1",
			transactionID: "tx-7",
			direction:     domain.DirectionCredit,
			amount:        amt0,
			balanceBefore: bal100,
			balanceAfter:  bal100,
			expectedErr:   domain.ErrInvalidAmount,
		},
		{
			name:          "invalid direction",
			id:            "entry-8",
			walletID:      "wallet-1",
			transactionID: "tx-8",
			direction:     domain.LedgerDirection("UNKNOWN"),
			amount:        amt10,
			balanceBefore: bal100,
			balanceAfter:  bal110,
			expectedErr:   domain.ErrInvalidLedgerDirection,
		},
		{
			name:          "empty id",
			id:            "",
			walletID:      "wallet-1",
			transactionID: "tx-1",
			direction:     domain.DirectionCredit,
			amount:        amt10,
			balanceBefore: bal100,
			balanceAfter:  bal110,
			expectedErr:   domain.ErrEmptyEntryID,
		},
		{
			name:          "empty wallet id",
			id:            "entry-1",
			walletID:      "",
			transactionID: "tx-1",
			direction:     domain.DirectionCredit,
			amount:        amt10,
			balanceBefore: bal100,
			balanceAfter:  bal110,
			expectedErr:   domain.ErrEmptyWalletID,
		},
		{
			name:          "empty transaction id",
			id:            "entry-1",
			walletID:      "wallet-1",
			transactionID: "",
			direction:     domain.DirectionCredit,
			amount:        amt10,
			balanceBefore: bal100,
			balanceAfter:  bal110,
			expectedErr:   domain.ErrEmptyTransactionID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, err := domain.NewWalletLedgerEntry(
				tt.id,
				tt.walletID,
				tt.transactionID,
				tt.direction,
				tt.amount,
				tt.balanceBefore,
				tt.balanceAfter,
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
			if entry.ID() != tt.id {
				t.Errorf("expected id %s, got %s", tt.id, entry.ID())
			}
			if entry.WalletID() != tt.walletID {
				t.Errorf("expected wallet id %s, got %s", tt.walletID, entry.WalletID())
			}
			if entry.TransactionID() != tt.transactionID {
				t.Errorf("expected transaction id %s, got %s", tt.transactionID, entry.TransactionID())
			}
			if entry.Direction() != tt.direction {
				t.Errorf("expected direction %s, got %s", tt.direction, entry.Direction())
			}
		})
	}
}
