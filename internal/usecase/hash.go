package usecase

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"backend-challenge-go-jungle-gaming/internal/domain"
)

type CanonicalWagerPayload struct {
	Amount                         string `json:"amount"`
	Currency                       string `json:"currency"`
	ExternalTransactionID          string `json:"externalTransactionId"`
	GameID                         string `json:"gameId"`
	Kind                           string `json:"kind"`
	PlayerID                       string `json:"playerId"`
	ProviderID                     string `json:"providerId"`
	ReferenceExternalTransactionID string `json:"referenceExternalTransactionId"`
	RoundID                        string `json:"roundId"`
	WalletID                       string `json:"walletId"`
}

func ComputePayloadHash(
	amount domain.Money,
	externalTxID string,
	gameID string,
	kind domain.WagerKind,
	playerID string,
	providerID string,
	refExternalTxID string,
	roundID string,
	walletID string,
) (string, error) {
	canonical := CanonicalWagerPayload{
		Amount:                         amount.AmountString(),
		Currency:                       amount.Currency(),
		ExternalTransactionID:          externalTxID,
		GameID:                         gameID,
		Kind:                           string(kind),
		PlayerID:                       playerID,
		ProviderID:                     providerID,
		ReferenceExternalTransactionID: refExternalTxID,
		RoundID:                        roundID,
		WalletID:                       walletID,
	}

	bytes, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("failed to marshal canonical payload: %w", err)
	}

	hash := sha256.Sum256(bytes)
	return hex.EncodeToString(hash[:]), nil
}
