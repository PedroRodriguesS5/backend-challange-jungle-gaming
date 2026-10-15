package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

var (
	ErrEmptyAmount        = errors.New("amount cannot be empty")
	ErrInvalidAmountStr   = errors.New("invalid amount format")
	ErrScientificNotation = errors.New("scientific notation is not allowed")
	ErrTooManyDecimals    = errors.New("too many decimal places")
	ErrEmptyCurrency      = errors.New("currency cannot be empty")
)

type Money struct {
	amount   decimal.Decimal
	currency string
}

func NewMoney(amountStr string, currency string) (Money, error) {
	if strings.TrimSpace(amountStr) == "" {
		return Money{}, ErrEmptyAmount
	}
	if strings.TrimSpace(currency) == "" {
		return Money{}, ErrEmptyCurrency
	}
	if strings.ContainsAny(amountStr, "eE") {
		return Money{}, ErrScientificNotation
	}

	amount, err := decimal.NewFromString(amountStr)
	if err != nil {
		return Money{}, ErrInvalidAmountStr
	}

	if amount.Exponent() < -2 {
		return Money{}, ErrTooManyDecimals
	}

	return Money{
		amount:   amount.Truncate(2),
		currency: strings.ToUpper(strings.TrimSpace(currency)),
	}, nil
}

func NewMoneyFromDecimal(amount decimal.Decimal, currency string) (Money, error) {
	if strings.TrimSpace(currency) == "" {
		return Money{}, ErrEmptyCurrency
	}
	if amount.Exponent() < -2 {
		return Money{}, ErrTooManyDecimals
	}
	return Money{
		amount:   amount.Truncate(2),
		currency: strings.ToUpper(strings.TrimSpace(currency)),
	}, nil
}

func ZeroMoney(currency string) Money {
	return Money{
		amount:   decimal.Zero,
		currency: strings.ToUpper(strings.TrimSpace(currency)),
	}
}

func (m Money) Amount() decimal.Decimal {
	return m.amount
}

func (m Money) AmountString() string {
	return m.amount.StringFixed(2)
}

func (m Money) Currency() string {
	return m.currency
}

func (m Money) IsZero() bool {
	return m.amount.IsZero()
}

func (m Money) IsPositive() bool {
	return m.amount.IsPositive()
}

func (m Money) IsNegative() bool {
	return m.amount.IsNegative()
}

func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, ErrCurrencyMismatch
	}
	return Money{
		amount:   m.amount.Add(other.amount),
		currency: m.currency,
	}, nil
}

func (m Money) Subtract(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, ErrCurrencyMismatch
	}
	return Money{
		amount:   m.amount.Sub(other.amount),
		currency: m.currency,
	}, nil
}

func (m Money) Negate() Money {
	return Money{
		amount:   m.amount.Neg(),
		currency: m.currency,
	}
}

func (m Money) Compare(other Money) (int, error) {
	if m.currency != other.currency {
		return 0, ErrCurrencyMismatch
	}
	return m.amount.Cmp(other.amount), nil
}

func (m Money) Equal(other Money) bool {
	return m.currency == other.currency && m.amount.Equal(other.amount)
}

func (m Money) GreaterThan(other Money) (bool, error) {
	cmp, err := m.Compare(other)
	if err != nil {
		return false, err
	}
	return cmp > 0, nil
}

func (m Money) LessThan(other Money) (bool, error) {
	cmp, err := m.Compare(other)
	if err != nil {
		return false, err
	}
	return cmp < 0, nil
}

func (m Money) String() string {
	return fmt.Sprintf("%s %s", m.AmountString(), m.currency)
}

func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	}{
		Amount:   m.AmountString(),
		Currency: m.currency,
	})
}

func (m *Money) UnmarshalJSON(b []byte) error {
	var temp struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	}
	if err := json.Unmarshal(b, &temp); err != nil {
		return err
	}
	parsed, err := NewMoney(temp.Amount, temp.Currency)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}
