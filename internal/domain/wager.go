package domain

import (
	"errors"
	"time"
)

type WagerKind string

const (
	KindOpening  WagerKind = "OPENING"
	KindBet      WagerKind = "BET"
	KindWin      WagerKind = "WIN"
	KindLoss     WagerKind = "LOSS"
	KindRefund   WagerKind = "REFUND"
	KindRollback WagerKind = "ROLLBACK"
)

type WagerStatus string

const (
	StatusPending          WagerStatus = "PENDING"
	StatusPendingReference WagerStatus = "PENDING_REFERENCE"
	StatusProcessed        WagerStatus = "PROCESSED"
	StatusRejected         WagerStatus = "REJECTED"
	StatusFailed           WagerStatus = "FAILED"
)

var (
	ErrInvalidWagerKind    = errors.New("invalid wager kind")
	ErrInvalidWagerStatus  = errors.New("invalid wager status")
	ErrEmptyExternalID     = errors.New("external transaction id cannot be empty")
	ErrEmptyProviderID     = errors.New("provider id cannot be empty")
	ErrEmptyIdempotencyKey = errors.New("idempotency key cannot be empty")
	ErrEmptyPayloadHash    = errors.New("payload hash cannot be empty")
)

type WagerTransaction struct {
	id                  string
	externalId          string
	providerId          string
	idempotencyKey      string
	payloadHash         string
	walletId            string
	playerId            string
	roundId             string
	gameId              string
	kind                WagerKind
	amount              Money
	referenceExternalId string
	status              WagerStatus
	failureCode         string
	balanceAfter        *Money
	createdAt           time.Time
	updatedAt           time.Time
}

func NewWagerTransaction(
	id string,
	externalId string,
	providerId string,
	idempotencyKey string,
	payloadHash string,
	walletId string,
	playerId string,
	roundId string,
	gameId string,
	kind WagerKind,
	amount Money,
	referenceExternalId string,
	now time.Time,
) (*WagerTransaction, error) {
	if id == "" {
		return nil, ErrEmptyTransactionID
	}
	if externalId == "" {
		return nil, ErrEmptyExternalID
	}
	if providerId == "" {
		return nil, ErrEmptyProviderID
	}
	if idempotencyKey == "" {
		return nil, ErrEmptyIdempotencyKey
	}
	if payloadHash == "" {
		return nil, ErrEmptyPayloadHash
	}
	if walletId == "" {
		return nil, ErrEmptyWalletID
	}
	if playerId == "" {
		return nil, ErrEmptyPlayerID
	}

	if kind == KindOpening {
		return nil, ErrInternalTransactionForbidden
	}

	switch kind {
	case KindBet, KindWin:
		if !amount.IsPositive() {
			return nil, ErrInvalidAmount
		}
	case KindLoss:
		if !amount.IsZero() {
			return nil, ErrInvalidLossAmount
		}
	case KindRefund, KindRollback:
		if !amount.IsPositive() {
			return nil, ErrInvalidAmount
		}
		if referenceExternalId == "" {
			return nil, ErrMissingReferenceID
		}
	default:
		return nil, ErrInvalidWagerKind
	}

	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	return &WagerTransaction{
		id:                  id,
		externalId:          externalId,
		providerId:          providerId,
		idempotencyKey:      idempotencyKey,
		payloadHash:         payloadHash,
		walletId:            walletId,
		playerId:            playerId,
		roundId:             roundId,
		gameId:              gameId,
		kind:                kind,
		amount:              amount,
		referenceExternalId: referenceExternalId,
		status:              StatusPending,
		failureCode:         "",
		createdAt:           now,
		updatedAt:           now,
	}, nil
}

func NewInternalOpeningTransaction(
	id string,
	walletId string,
	playerId string,
	amount Money,
	now time.Time,
) (*WagerTransaction, error) {
	if id == "" {
		return nil, ErrEmptyTransactionID
	}
	if walletId == "" {
		return nil, ErrEmptyWalletID
	}
	if playerId == "" {
		return nil, ErrEmptyPlayerID
	}
	if !amount.IsPositive() {
		return nil, ErrInvalidAmount
	}

	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	openingBalance := amount
	return &WagerTransaction{
		id:                  id,
		externalId:          "INTERNAL_OPENING_" + id,
		providerId:          "INTERNAL",
		idempotencyKey:      "INTERNAL_OPENING_" + id,
		payloadHash:         "INTERNAL",
		walletId:            walletId,
		playerId:            playerId,
		roundId:             "",
		gameId:              "",
		kind:                KindOpening,
		amount:              amount,
		referenceExternalId: "",
		status:              StatusProcessed,
		failureCode:         "",
		balanceAfter:        &openingBalance,
		createdAt:           now,
		updatedAt:           now,
	}, nil
}

func RehydrateWagerTransaction(
	id string,
	externalId string,
	providerId string,
	idempotencyKey string,
	payloadHash string,
	walletId string,
	playerId string,
	roundId string,
	gameId string,
	kind WagerKind,
	amount Money,
	referenceExternalId string,
	status WagerStatus,
	failureCode string,
	balanceAfter *Money,
	createdAt time.Time,
	updatedAt time.Time,
) (*WagerTransaction, error) {
	if id == "" {
		return nil, ErrEmptyTransactionID
	}
	return &WagerTransaction{
		id:                  id,
		externalId:          externalId,
		providerId:          providerId,
		idempotencyKey:      idempotencyKey,
		payloadHash:         payloadHash,
		walletId:            walletId,
		playerId:            playerId,
		roundId:             roundId,
		gameId:              gameId,
		kind:                kind,
		amount:              amount,
		referenceExternalId: referenceExternalId,
		status:              status,
		failureCode:         failureCode,
		balanceAfter:        balanceAfter,
		createdAt:           createdAt.UTC(),
		updatedAt:           updatedAt.UTC(),
	}, nil
}

func (w *WagerTransaction) ID() string {
	return w.id
}

func (w *WagerTransaction) ExternalID() string {
	return w.externalId
}

func (w *WagerTransaction) ProviderID() string {
	return w.providerId
}

func (w *WagerTransaction) IdempotencyKey() string {
	return w.idempotencyKey
}

func (w *WagerTransaction) PayloadHash() string {
	return w.payloadHash
}

func (w *WagerTransaction) WalletID() string {
	return w.walletId
}

func (w *WagerTransaction) PlayerID() string {
	return w.playerId
}

func (w *WagerTransaction) RoundID() string {
	return w.roundId
}

func (w *WagerTransaction) GameID() string {
	return w.gameId
}

func (w *WagerTransaction) Kind() WagerKind {
	return w.kind
}

func (w *WagerTransaction) Amount() Money {
	return w.amount
}

func (w *WagerTransaction) ReferenceExternalID() string {
	return w.referenceExternalId
}

func (w *WagerTransaction) Status() WagerStatus {
	return w.status
}

func (w *WagerTransaction) FailureCode() string {
	return w.failureCode
}

func (w *WagerTransaction) BalanceAfter() *Money {
	return w.balanceAfter
}

func (w *WagerTransaction) SetBalanceAfter(m Money) {
	w.balanceAfter = &m
}

func (w *WagerTransaction) CreatedAt() time.Time {
	return w.createdAt
}

func (w *WagerTransaction) UpdatedAt() time.Time {
	return w.updatedAt
}

func (w *WagerTransaction) IsTerminal() bool {
	return w.status == StatusProcessed || w.status == StatusRejected || w.status == StatusFailed
}

func (w *WagerTransaction) TransitionToProcessed(now time.Time) error {
	if w.IsTerminal() {
		return ErrTerminalState
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	w.status = StatusProcessed
	w.updatedAt = now
	return nil
}

func (w *WagerTransaction) TransitionToPendingReference(now time.Time) error {
	if w.IsTerminal() {
		return ErrTerminalState
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	w.status = StatusPendingReference
	w.updatedAt = now
	return nil
}

func (w *WagerTransaction) TransitionToRejected(failureCode string, now time.Time) error {
	if w.IsTerminal() {
		return ErrTerminalState
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	w.status = StatusRejected
	w.failureCode = failureCode
	w.updatedAt = now
	return nil
}

func (w *WagerTransaction) TransitionToFailed(failureCode string, now time.Time) error {
	if w.IsTerminal() {
		return ErrTerminalState
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	w.status = StatusFailed
	w.failureCode = failureCode
	w.updatedAt = now
	return nil
}

func (w *WagerTransaction) ValidateReference(orig *WagerTransaction) error {
	if orig == nil {
		return ErrReferenceNotFound
	}
	if orig.Status() != StatusProcessed {
		return ErrReferenceMismatch
	}
	if w.providerId != orig.ProviderID() {
		return ErrReferenceMismatch
	}
	if w.playerId != orig.PlayerID() {
		return ErrReferenceMismatch
	}
	if w.walletId != orig.WalletID() {
		return ErrReferenceMismatch
	}
	if w.roundId != orig.RoundID() {
		return ErrReferenceMismatch
	}
	if w.amount.Currency() != orig.Amount().Currency() {
		return ErrCurrencyMismatch
	}
	if !w.amount.Equal(orig.Amount()) {
		return ErrReferenceMismatch
	}
	return nil
}
