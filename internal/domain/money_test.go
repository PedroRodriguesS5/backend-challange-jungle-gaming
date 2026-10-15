package domain_test

import (
	"encoding/json"
	"errors"
	"testing"

	"backend-challenge-go-jungle-gaming/internal/domain"
)

func TestNewMoney(t *testing.T) {
	tests := []struct {
		name        string
		amountStr   string
		currency    string
		expectedAmt string
		expectedErr error
	}{
		{
			name:        "valid whole amount",
			amountStr:   "100",
			currency:    "BRL",
			expectedAmt: "100.00",
			expectedErr: nil,
		},
		{
			name:        "valid two decimal places",
			amountStr:   "25.50",
			currency:    "BRL",
			expectedAmt: "25.50",
			expectedErr: nil,
		},
		{
			name:        "valid one decimal place",
			amountStr:   "25.5",
			currency:    "brl",
			expectedAmt: "25.50",
			expectedErr: nil,
		},
		{
			name:        "valid zero amount",
			amountStr:   "0.00",
			currency:    "USD",
			expectedAmt: "0.00",
			expectedErr: nil,
		},
		{
			name:        "empty amount",
			amountStr:   "",
			currency:    "BRL",
			expectedErr: domain.ErrEmptyAmount,
		},
		{
			name:        "empty currency",
			amountStr:   "10.00",
			currency:    "",
			expectedErr: domain.ErrEmptyCurrency,
		},
		{
			name:        "scientific notation lowercase",
			amountStr:   "1e2",
			currency:    "BRL",
			expectedErr: domain.ErrScientificNotation,
		},
		{
			name:        "scientific notation uppercase",
			amountStr:   "1E2",
			currency:    "BRL",
			expectedErr: domain.ErrScientificNotation,
		},
		{
			name:        "too many decimal places",
			amountStr:   "10.005",
			currency:    "BRL",
			expectedErr: domain.ErrTooManyDecimals,
		},
		{
			name:        "invalid characters",
			amountStr:   "abc",
			currency:    "BRL",
			expectedErr: domain.ErrInvalidAmountStr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := domain.NewMoney(tt.amountStr, tt.currency)
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
			if m.AmountString() != tt.expectedAmt {
				t.Errorf("expected amount %s, got %s", tt.expectedAmt, m.AmountString())
			}
		})
	}
}

func TestMoney_Arithmetic(t *testing.T) {
	t.Run("add success", func(t *testing.T) {
		m1, err := domain.NewMoney("100.50", "BRL")
		if err != nil {
			t.Fatal(err)
		}
		m2, err := domain.NewMoney("50.25", "BRL")
		if err != nil {
			t.Fatal(err)
		}
		res, err := m1.Add(m2)
		if err != nil {
			t.Fatal(err)
		}
		if res.AmountString() != "150.75" {
			t.Errorf("expected 150.75, got %s", res.AmountString())
		}
	})

	t.Run("add currency mismatch", func(t *testing.T) {
		m1, _ := domain.NewMoney("10.00", "BRL")
		m2, _ := domain.NewMoney("10.00", "USD")
		_, err := m1.Add(m2)
		if !errors.Is(err, domain.ErrCurrencyMismatch) {
			t.Errorf("expected ErrCurrencyMismatch, got %v", err)
		}
	})

	t.Run("subtract success", func(t *testing.T) {
		m1, _ := domain.NewMoney("100.00", "BRL")
		m2, _ := domain.NewMoney("30.00", "BRL")
		res, err := m1.Subtract(m2)
		if err != nil {
			t.Fatal(err)
		}
		if res.AmountString() != "70.00" {
			t.Errorf("expected 70.00, got %s", res.AmountString())
		}
	})

	t.Run("subtract currency mismatch", func(t *testing.T) {
		m1, _ := domain.NewMoney("10.00", "BRL")
		m2, _ := domain.NewMoney("10.00", "USD")
		_, err := m1.Subtract(m2)
		if !errors.Is(err, domain.ErrCurrencyMismatch) {
			t.Errorf("expected ErrCurrencyMismatch, got %v", err)
		}
	})

	t.Run("negate", func(t *testing.T) {
		m, _ := domain.NewMoney("25.00", "BRL")
		neg := m.Negate()
		if neg.AmountString() != "-25.00" {
			t.Errorf("expected -25.00, got %s", neg.AmountString())
		}
		if !neg.IsNegative() {
			t.Error("expected IsNegative to be true")
		}
	})

	t.Run("compare and equal", func(t *testing.T) {
		m1, _ := domain.NewMoney("100.00", "BRL")
		m2, _ := domain.NewMoney("50.00", "BRL")
		m3, _ := domain.NewMoney("100.00", "BRL")
		m4, _ := domain.NewMoney("100.00", "USD")

		gt, err := m1.GreaterThan(m2)
		if err != nil || !gt {
			t.Errorf("expected m1 > m2, got %v, err: %v", gt, err)
		}

		lt, err := m2.LessThan(m1)
		if err != nil || !lt {
			t.Errorf("expected m2 < m1, got %v, err: %v", lt, err)
		}

		if !m1.Equal(m3) {
			t.Error("expected m1 == m3")
		}

		if m1.Equal(m4) {
			t.Error("expected m1 != m4 (different currency)")
		}

		_, err = m1.Compare(m4)
		if !errors.Is(err, domain.ErrCurrencyMismatch) {
			t.Errorf("expected ErrCurrencyMismatch, got %v", err)
		}
	})

	t.Run("zero money", func(t *testing.T) {
		z := domain.ZeroMoney("BRL")
		if !z.IsZero() {
			t.Error("expected IsZero to be true")
		}
		if z.AmountString() != "0.00" {
			t.Errorf("expected 0.00, got %s", z.AmountString())
		}
		if z.Currency() != "BRL" {
			t.Errorf("expected BRL, got %s", z.Currency())
		}
	})
}

func TestMoney_JSON(t *testing.T) {
	m, err := domain.NewMoney("25.00", "BRL")
	if err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	expected := `{"amount":"25.00","currency":"BRL"}`
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}

	var parsed domain.Money
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	if !parsed.Equal(m) {
		t.Errorf("expected %v, got %v", m, parsed)
	}
}
