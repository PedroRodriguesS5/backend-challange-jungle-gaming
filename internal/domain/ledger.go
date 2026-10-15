package domain

import (
	"errors"
	"time"
)

type LedgerDirection string

const (
	DirectionDebit  LedgerDirection = "DEBIT"
	DirectionCredit LedgerDirection = "CREDIT"
)

var (
	ErrEmptyEntryID       = errors.New("ledger entry id cannot be empty")
	ErrEmptyWalletID      = errors.New("wallet id cannot be empty")
	ErrEmptyTransactionID = errors.New("transaction id cannot be empty")
)

type WalletLedgerEntry struct {
	id            string
	walletId      string
	transactionId string
	direction     LedgerDirection
	amount        Money
	balanceBefore Money
	balanceAfter  Money
	createdAt     time.Time
}

func NewWalletLedgerEntry(
	id string,
	walletId string,
	transactionId string,
	direction LedgerDirection,
	amount Money,
	balanceBefore Money,
	balanceAfter Money,
	createdAt time.Time,
) (WalletLedgerEntry, error) {
	if id == "" {
		return WalletLedgerEntry{}, ErrEmptyEntryID
	}
	if walletId == "" {
		return WalletLedgerEntry{}, ErrEmptyWalletID
	}
	if transactionId == "" {
		return WalletLedgerEntry{}, ErrEmptyTransactionID
	}

	if direction != DirectionDebit && direction != DirectionCredit {
		return WalletLedgerEntry{}, ErrInvalidLedgerDirection
	}

	if amount.IsZero() || amount.IsNegative() {
		return WalletLedgerEntry{}, ErrInvalidAmount
	}

	if amount.Currency() != balanceBefore.Currency() || amount.Currency() != balanceAfter.Currency() {
		return WalletLedgerEntry{}, ErrCurrencyMismatch
	}

	switch direction {
	case DirectionCredit:
		expected, err := balanceBefore.Add(amount)
		if err != nil || !expected.Equal(balanceAfter) {
			return WalletLedgerEntry{}, ErrInvalidLedgerMath
		}
	case DirectionDebit:
		expected, err := balanceBefore.Subtract(amount)
		if err != nil || !expected.Equal(balanceAfter) {
			return WalletLedgerEntry{}, ErrInvalidLedgerMath
		}
		if balanceAfter.IsNegative() {
			return WalletLedgerEntry{}, ErrNegativeBalance
		}
	}

	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	return WalletLedgerEntry{
		id:            id,
		walletId:      walletId,
		transactionId: transactionId,
		direction:     direction,
		amount:        amount,
		balanceBefore: balanceBefore,
		balanceAfter:  balanceAfter,
		createdAt:     createdAt.UTC(),
	}, nil
}

func (e WalletLedgerEntry) ID() string {
	return e.id
}

func (e WalletLedgerEntry) WalletID() string {
	return e.walletId
}

func (e WalletLedgerEntry) TransactionID() string {
	return e.transactionId
}

func (e WalletLedgerEntry) Direction() LedgerDirection {
	return e.direction
}

func (e WalletLedgerEntry) Amount() Money {
	return e.amount
}

func (e WalletLedgerEntry) BalanceBefore() Money {
	return e.balanceBefore
}

func (e WalletLedgerEntry) BalanceAfter() Money {
	return e.balanceAfter
}

func (e WalletLedgerEntry) CreatedAt() time.Time {
	return e.createdAt
}
