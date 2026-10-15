package usecase_test

import (
	"testing"

	"backend-challenge-go-jungle-gaming/internal/domain"
	"backend-challenge-go-jungle-gaming/internal/usecase"
)

func TestComputePayloadHash_Deterministic(t *testing.T) {
	amt, _ := domain.NewMoney("25.00", "BRL")

	h1, err := usecase.ComputePayloadHash(
		amt,
		"ext-1",
		"game-1",
		domain.KindBet,
		"player-1",
		"provider-a",
		"",
		"round-1",
		"wallet-1",
	)
	if err != nil {
		t.Fatal(err)
	}

	h2, err := usecase.ComputePayloadHash(
		amt,
		"ext-1",
		"game-1",
		domain.KindBet,
		"player-1",
		"provider-a",
		"",
		"round-1",
		"wallet-1",
	)
	if err != nil {
		t.Fatal(err)
	}

	if h1 != h2 {
		t.Errorf("expected deterministic hashes, got %s and %s", h1, h2)
	}

	// Change amount
	amtDiff, _ := domain.NewMoney("26.00", "BRL")
	h3, _ := usecase.ComputePayloadHash(
		amtDiff,
		"ext-1",
		"game-1",
		domain.KindBet,
		"player-1",
		"provider-a",
		"",
		"round-1",
		"wallet-1",
	)
	if h1 == h3 {
		t.Error("expected different hash for different amount")
	}
}
