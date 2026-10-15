package domain

import (
	"errors"
	"time"
)

var (
	ErrEmptyPlayerID = errors.New("player id cannot be empty")
)

type Wallet struct {
	id        string
	playerId  string
	balance   Money
	version   int64
	createdAt time.Time
	updatedAt time.Time
}

func NewWallet(id string, playerId string, initialBalance Money, now time.Time) (*Wallet, error) {
	if id == "" {
		return nil, ErrEmptyWalletID
	}
	if playerId == "" {
		return nil, ErrEmptyPlayerID
	}
	if initialBalance.IsNegative() {
		return nil, ErrInvalidInitialBalance
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	return &Wallet{
		id:        id,
		playerId:  playerId,
		balance:   initialBalance,
		version:   1,
		createdAt: now,
		updatedAt: now,
	}, nil
}

func RehydrateWallet(
	id string,
	playerId string,
	balance Money,
	version int64,
	createdAt time.Time,
	updatedAt time.Time,
) (*Wallet, error) {
	if id == "" {
		return nil, ErrEmptyWalletID
	}
	if playerId == "" {
		return nil, ErrEmptyPlayerID
	}
	if balance.IsNegative() {
		return nil, ErrNegativeBalance
	}
	if version < 1 {
		return nil, ErrInvalidVersion
	}

	return &Wallet{
		id:        id,
		playerId:  playerId,
		balance:   balance,
		version:   version,
		createdAt: createdAt.UTC(),
		updatedAt: updatedAt.UTC(),
	}, nil
}

func (w *Wallet) ID() string {
	return w.id
}

func (w *Wallet) PlayerID() string {
	return w.playerId
}

func (w *Wallet) Currency() string {
	return w.balance.Currency()
}

func (w *Wallet) Balance() Money {
	return w.balance
}

func (w *Wallet) Version() int64 {
	return w.version
}

func (w *Wallet) CreatedAt() time.Time {
	return w.createdAt
}

func (w *Wallet) UpdatedAt() time.Time {
	return w.updatedAt
}

func (w *Wallet) Debit(transactionId string, ledgerEntryId string, amount Money, now time.Time) (WalletLedgerEntry, error) {
	if amount.Currency() != w.balance.Currency() {
		return WalletLedgerEntry{}, ErrCurrencyMismatch
	}
	if amount.IsNegative() || amount.IsZero() {
		return WalletLedgerEntry{}, ErrInvalidAmount
	}

	lt, err := w.balance.LessThan(amount)
	if err != nil {
		return WalletLedgerEntry{}, err
	}
	if lt {
		return WalletLedgerEntry{}, ErrInsufficientFunds
	}

	before := w.balance
	after, err := before.Subtract(amount)
	if err != nil {
		return WalletLedgerEntry{}, err
	}

	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	entry, err := NewWalletLedgerEntry(
		ledgerEntryId,
		w.id,
		transactionId,
		DirectionDebit,
		amount,
		before,
		after,
		now,
	)
	if err != nil {
		return WalletLedgerEntry{}, err
	}

	w.balance = after
	w.version++
	w.updatedAt = now

	return entry, nil
}

func (w *Wallet) Credit(transactionId string, ledgerEntryId string, amount Money, now time.Time) (WalletLedgerEntry, error) {
	if amount.Currency() != w.balance.Currency() {
		return WalletLedgerEntry{}, ErrCurrencyMismatch
	}
	if amount.IsNegative() || amount.IsZero() {
		return WalletLedgerEntry{}, ErrInvalidAmount
	}

	before := w.balance
	after, err := before.Add(amount)
	if err != nil {
		return WalletLedgerEntry{}, err
	}

	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	entry, err := NewWalletLedgerEntry(
		ledgerEntryId,
		w.id,
		transactionId,
		DirectionCredit,
		amount,
		before,
		after,
		now,
	)
	if err != nil {
		return WalletLedgerEntry{}, err
	}

	w.balance = after
	w.version++
	w.updatedAt = now

	return entry, nil
}

func (w *Wallet) CreateOpeningLedgerEntry(transactionId string, entryId string, now time.Time) (WalletLedgerEntry, error) {
	if w.balance.IsZero() {
		return WalletLedgerEntry{}, ErrInvalidAmount
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	zero := ZeroMoney(w.Currency())
	return NewWalletLedgerEntry(
		entryId,
		w.id,
		transactionId,
		DirectionCredit,
		w.balance,
		zero,
		w.balance,
		now,
	)
}
