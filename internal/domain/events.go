package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	EventTypeWagerTransactionProcessed        = "WagerTransactionProcessed"
	EventTypeWagerTransactionRejected         = "WagerTransactionRejected"
	EventTypeWalletBalanceChanged             = "WalletBalanceChanged"
	EventTypeWagerTransactionPendingReference = "WagerTransactionPendingReference"
)

type EventEnvelope struct {
	EventID       string          `json:"eventId"`
	EventType     string          `json:"eventType"`
	AggregateID   string          `json:"aggregateId"`
	CorrelationID string          `json:"correlationId"`
	CausationID   string          `json:"causationId,omitempty"`
	OccurredAt    string          `json:"occurredAt"`
	Version       int             `json:"version"`
	Data          json.RawMessage `json:"data"`
}

type WagerTransactionProcessedData struct {
	TransactionID       string `json:"transactionId"`
	ExternalID          string `json:"externalId"`
	ProviderID          string `json:"providerId"`
	WalletID            string `json:"walletId"`
	PlayerID            string `json:"playerId"`
	RoundID             string `json:"roundId,omitempty"`
	GameID              string `json:"gameId,omitempty"`
	Kind                string `json:"kind"`
	Amount              Money  `json:"amount"`
	ReferenceExternalID string `json:"referenceExternalId,omitempty"`
	Status              string `json:"status"`
}

type WagerTransactionRejectedData struct {
	TransactionID       string `json:"transactionId"`
	ExternalID          string `json:"externalId"`
	ProviderID          string `json:"providerId"`
	WalletID            string `json:"walletId"`
	PlayerID            string `json:"playerId"`
	Kind                string `json:"kind"`
	Amount              Money  `json:"amount"`
	ReferenceExternalID string `json:"referenceExternalId,omitempty"`
	FailureCode         string `json:"failureCode"`
}

type WalletBalanceChangedData struct {
	WalletID      string `json:"walletId"`
	TransactionID string `json:"transactionId"`
	Direction     string `json:"direction"`
	Money         Money  `json:"money"`
	BalanceBefore Money  `json:"balanceBefore"`
	BalanceAfter  Money  `json:"balanceAfter"`
	WalletVersion int64  `json:"walletVersion"`
}

type WagerTransactionPendingReferenceData struct {
	TransactionID       string `json:"transactionId"`
	ExternalID          string `json:"externalId"`
	ProviderID          string `json:"providerId"`
	WalletID            string `json:"walletId"`
	PlayerID            string `json:"playerId"`
	Kind                string `json:"kind"`
	Amount              Money  `json:"amount"`
	ReferenceExternalID string `json:"referenceExternalId"`
}

func NewEventEnvelope(
	eventType string,
	aggregateID string,
	correlationID string,
	causationID string,
	occurredAt time.Time,
	version int,
	data any,
) (*EventEnvelope, error) {
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	} else {
		occurredAt = occurredAt.UTC()
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	if correlationID == "" {
		correlationID = uuid.NewString()
	}

	return &EventEnvelope{
		EventID:       uuid.NewString(),
		EventType:     eventType,
		AggregateID:   aggregateID,
		CorrelationID: correlationID,
		CausationID:   causationID,
		OccurredAt:    occurredAt.Format(time.RFC3339Nano),
		Version:       version,
		Data:          dataBytes,
	}, nil
}
