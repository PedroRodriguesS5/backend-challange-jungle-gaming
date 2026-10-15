package domain

import (
	"errors"
	"fmt"
)

var (
	ErrInsufficientFunds            = errors.New("insufficient funds")
	ErrCurrencyMismatch             = errors.New("currency mismatch")
	ErrInvalidStateTransition       = errors.New("invalid state transition")
	ErrIdempotencyConflict          = errors.New("idempotency conflict")
	ErrUnauthorizedProvider         = errors.New("unauthorized provider")
	ErrInvalidAmount                = errors.New("invalid amount")
	ErrNegativeBalance              = errors.New("wallet balance cannot be negative")
	ErrTerminalState                = errors.New("transaction in terminal state cannot be modified")
	ErrWalletNotFound               = errors.New("wallet not found")
	ErrTransactionNotFound          = errors.New("transaction not found")
	ErrReferenceNotFound            = errors.New("reference transaction not found")
	ErrReferenceMismatch            = errors.New("reference transaction attributes mismatch")
	ErrReferenceAlreadyReversed     = errors.New("reference transaction already reversed")
	ErrInternalTransactionForbidden = errors.New("internal transaction type is not allowed for external operations")
	ErrInvalidLedgerMath            = errors.New("ledger balance math does not match direction and amount")
	ErrInvalidLedgerDirection       = errors.New("invalid ledger entry direction")
	ErrReversalExceedsBalance       = errors.New("reversal would cause negative wallet balance")
	ErrInvalidInitialBalance        = errors.New("initial balance must be non-negative")
	ErrInvalidVersion               = errors.New("wallet version must be positive")
	ErrMissingReferenceID           = errors.New("reference external transaction id is required")
	ErrInvalidLossAmount            = errors.New("loss transaction amount must be exactly 0.00")
)

const (
	FailureCodeInsufficientFunds        = "INSUFFICIENT_FUNDS"
	FailureCodeReferenceNotFound        = "REFERENCE_NOT_FOUND"
	FailureCodeReferenceMismatch        = "REFERENCE_MISMATCH"
	FailureCodeReferenceAlreadyReversed = "REFERENCE_ALREADY_REVERSED"
	FailureCodeReversalExceedsBalance   = "REVERSAL_EXCEEDS_BALANCE"
	FailureCodeInvalidAmount            = "INVALID_AMOUNT"
	FailureCodeCurrencyMismatch         = "CURRENCY_MISMATCH"
	FailureCodeInternalOnly             = "INTERNAL_TRANSACTION_TYPE_FORBIDDEN"
	FailureCodeIdempotencyConflict      = "IDEMPOTENCY_CONFLICT"
	FailureCodeUnauthorizedProvider     = "UNAUTHORIZED_PROVIDER"
)

type DomainError struct {
	Code    string
	Message string
	Err     error
}

func (e *DomainError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *DomainError) Unwrap() error {
	return e.Err
}

func (e *DomainError) Is(target error) bool {
	if target == nil {
		return false
	}
	if t, ok := target.(*DomainError); ok {
		return e.Code == t.Code
	}
	if e.Err != nil {
		return errors.Is(e.Err, target)
	}
	return false
}

func NewDomainError(code string, message string, err error) *DomainError {
	return &DomainError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}
