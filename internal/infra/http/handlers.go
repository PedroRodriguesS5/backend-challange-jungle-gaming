package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"backend-challenge-go-jungle-gaming/internal/domain"
	"backend-challenge-go-jungle-gaming/internal/infra/metrics"
	"backend-challenge-go-jungle-gaming/internal/usecase"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handlers struct {
	pool              *pgxpool.Pool
	sqsClient         *sqs.Client
	sqsQueueURL       string
	createWalletUC    *usecase.CreateWalletUseCase
	getWalletUC       *usecase.GetWalletUseCase
	listLedgerUC      *usecase.ListLedgerUseCase
	processWagerUC    *usecase.ProcessWagerUseCase
	getTransactionUC  *usecase.GetTransactionUseCase
	reconcileWalletUC *usecase.ReconcileWalletUseCase
}

func NewHandlers(
	pool *pgxpool.Pool,
	sqsClient *sqs.Client,
	sqsQueueURL string,
	createWalletUC *usecase.CreateWalletUseCase,
	getWalletUC *usecase.GetWalletUseCase,
	listLedgerUC *usecase.ListLedgerUseCase,
	processWagerUC *usecase.ProcessWagerUseCase,
	getTransactionUC *usecase.GetTransactionUseCase,
	reconcileWalletUC *usecase.ReconcileWalletUseCase,
) *Handlers {
	return &Handlers{
		pool:              pool,
		sqsClient:         sqsClient,
		sqsQueueURL:       sqsQueueURL,
		createWalletUC:    createWalletUC,
		getWalletUC:       getWalletUC,
		listLedgerUC:      listLedgerUC,
		processWagerUC:    processWagerUC,
		getTransactionUC:  getTransactionUC,
		reconcileWalletUC: reconcileWalletUC,
	}
}

type CreateWalletRequest struct {
	PlayerID       string       `json:"playerId"`
	InitialBalance domain.Money `json:"initialBalance"`
}

func (h *Handlers) HandleCreateWallet(w http.ResponseWriter, r *http.Request) {
	if !IsInternalCaller(r.Context()) {
		writeJSONError(w, http.StatusForbidden, "wallet operations are restricted to internal service")
		return
	}

	var req CreateWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	out, err := h.createWalletUC.Execute(r.Context(), usecase.CreateWalletInput{
		PlayerID:       req.PlayerID,
		InitialBalance: req.InitialBalance,
	})
	if err != nil {
		var domErr *domain.DomainError
		if errors.As(err, &domErr) && domErr.Code == domain.FailureCodeIdempotencyConflict {
			writeJSONError(w, http.StatusConflict, domErr.Message)
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, out)
}

func (h *Handlers) HandleGetWallet(w http.ResponseWriter, r *http.Request) {
	if !IsInternalCaller(r.Context()) {
		writeJSONError(w, http.StatusForbidden, "wallet operations are restricted to internal service")
		return
	}

	walletID := r.PathValue("walletId")
	if walletID == "" {
		writeJSONError(w, http.StatusBadRequest, "walletId is required")
		return
	}

	wallet, err := h.getWalletUC.Execute(r.Context(), walletID)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			writeJSONError(w, http.StatusNotFound, "wallet not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":        wallet.ID(),
		"playerId":  wallet.PlayerID(),
		"balance":   wallet.Balance(),
		"version":   wallet.Version(),
		"createdAt": wallet.CreatedAt(),
		"updatedAt": wallet.UpdatedAt(),
	})
}

func (h *Handlers) HandleListLedger(w http.ResponseWriter, r *http.Request) {
	if !IsInternalCaller(r.Context()) {
		writeJSONError(w, http.StatusForbidden, "wallet operations are restricted to internal service")
		return
	}

	walletID := r.PathValue("walletId")
	if walletID == "" {
		writeJSONError(w, http.StatusBadRequest, "walletId is required")
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	out, err := h.listLedgerUC.Execute(r.Context(), walletID, cursor, limit)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type ledgerEntryDTO struct {
		ID            string                 `json:"id"`
		WalletID      string                 `json:"walletId"`
		TransactionID string                 `json:"transactionId"`
		Direction     domain.LedgerDirection `json:"direction"`
		Amount        domain.Money           `json:"amount"`
		BalanceBefore domain.Money           `json:"balanceBefore"`
		BalanceAfter  domain.Money           `json:"balanceAfter"`
		CreatedAt     string                 `json:"createdAt"`
	}

	entriesDTO := make([]ledgerEntryDTO, len(out.Entries))
	for i, e := range out.Entries {
		entriesDTO[i] = ledgerEntryDTO{
			ID:            e.ID(),
			WalletID:      e.WalletID(),
			TransactionID: e.TransactionID(),
			Direction:     e.Direction(),
			Amount:        e.Amount(),
			BalanceBefore: e.BalanceBefore(),
			BalanceAfter:  e.BalanceAfter(),
			CreatedAt:     e.CreatedAt().Format("2006-01-02T15:04:05.999999999Z07:00"),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entries":    entriesDTO,
		"nextCursor": out.NextCursor,
	})
}

type ProcessWagerRequest struct {
	ProviderID                     string           `json:"providerId"`
	ExternalTransactionID          string           `json:"externalTransactionId"`
	PlayerID                       string           `json:"playerId"`
	WalletID                       string           `json:"walletId"`
	RoundID                        string           `json:"roundId"`
	GameID                         string           `json:"gameId"`
	Kind                           domain.WagerKind `json:"kind"`
	Money                          domain.Money     `json:"money"`
	ReferenceExternalTransactionID string           `json:"referenceExternalTransactionId,omitempty"`
}

func (h *Handlers) HandleProcessWager(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if strings.TrimSpace(idempotencyKey) == "" {
		writeJSONError(w, http.StatusBadRequest, "Idempotency-Key header is required")
		return
	}

	authProvider, ok := GetAuthenticatedProvider(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	var req ProcessWagerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if !IsInternalCaller(r.Context()) && req.ProviderID != authProvider {
		writeJSONError(w, http.StatusForbidden, "provider ID in body does not match authenticated provider")
		return
	}

	out, err := h.processWagerUC.Execute(r.Context(), usecase.ProcessWagerInput{
		ProviderID:                     req.ProviderID,
		ExternalTransactionID:          req.ExternalTransactionID,
		IdempotencyKey:                 idempotencyKey,
		PlayerID:                       req.PlayerID,
		WalletID:                       req.WalletID,
		RoundID:                        req.RoundID,
		GameID:                         req.GameID,
		Kind:                           req.Kind,
		Money:                          req.Money,
		ReferenceExternalTransactionID: req.ReferenceExternalTransactionID,
	})
	if err != nil {
		var domErr *domain.DomainError
		if errors.As(err, &domErr) {
			if domErr.Code == domain.FailureCodeIdempotencyConflict {
				writeJSONError(w, http.StatusConflict, domErr.Message)
				return
			}
			if domErr.Code == domain.FailureCodeInternalOnly {
				writeJSONError(w, http.StatusBadRequest, domErr.Message)
				return
			}
			if domErr.Code == domain.FailureCodeUnauthorizedProvider {
				writeJSONError(w, http.StatusForbidden, domErr.Message)
				return
			}
		}
		if errors.Is(err, domain.ErrInternalTransactionForbidden) || errors.Is(err, domain.ErrInvalidWagerKind) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	switch out.Status {
	case domain.StatusProcessed:
		writeJSON(w, http.StatusOK, out)
	case domain.StatusPendingReference:
		writeJSON(w, http.StatusAccepted, out)
	case domain.StatusRejected:
		writeJSON(w, http.StatusUnprocessableEntity, out)
	default:
		writeJSON(w, http.StatusOK, out)
	}
}

func (h *Handlers) HandleGetTransaction(w http.ResponseWriter, r *http.Request) {
	txID := r.PathValue("transactionId")
	if txID == "" {
		writeJSONError(w, http.StatusBadRequest, "transactionId is required")
		return
	}

	authProvider, _ := GetAuthenticatedProvider(r.Context())
	tx, err := h.getTransactionUC.GetByID(r.Context(), txID)
	if err != nil {
		if errors.Is(err, domain.ErrTransactionNotFound) {
			writeJSONError(w, http.StatusNotFound, "transaction not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if !IsInternalCaller(r.Context()) && tx.ProviderID() != authProvider {
		writeJSONError(w, http.StatusNotFound, "transaction not found")
		return
	}

	writeJSON(w, http.StatusOK, txToDTO(tx))
}

func (h *Handlers) HandleGetTransactionByProviderAndExternalID(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("providerId")
	externalID := r.PathValue("externalTransactionId")
	if providerID == "" || externalID == "" {
		writeJSONError(w, http.StatusBadRequest, "providerId and externalTransactionId are required")
		return
	}

	authProvider, _ := GetAuthenticatedProvider(r.Context())
	if !IsInternalCaller(r.Context()) && providerID != authProvider {
		writeJSONError(w, http.StatusForbidden, "cannot access transactions for other providers")
		return
	}

	tx, err := h.getTransactionUC.GetByProviderAndExternalID(r.Context(), providerID, externalID)
	if err != nil {
		if errors.Is(err, domain.ErrTransactionNotFound) {
			writeJSONError(w, http.StatusNotFound, "transaction not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, txToDTO(tx))
}

func (h *Handlers) HandleReconciliation(w http.ResponseWriter, r *http.Request) {
	if !IsInternalCaller(r.Context()) {
		writeJSONError(w, http.StatusForbidden, "wallet operations are restricted to internal service")
		return
	}

	walletID := r.PathValue("walletId")
	if walletID == "" {
		writeJSONError(w, http.StatusBadRequest, "walletId is required")
		return
	}

	out, err := h.reconcileWalletUC.Execute(r.Context(), walletID)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			writeJSONError(w, http.StatusNotFound, "wallet not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) HandleHealthLive(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "UP"})
}

func (h *Handlers) HandleHealthReady(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "database pool not initialized")
		return
	}

	if err := h.pool.Ping(r.Context()); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "DOWN",
			"error":  "database unavailable: " + err.Error(),
		})
		return
	}

	if h.sqsClient != nil && h.sqsQueueURL != "" {
		_, err := h.sqsClient.GetQueueAttributes(r.Context(), &sqs.GetQueueAttributesInput{
			QueueUrl:       aws.String(h.sqsQueueURL),
			AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameApproximateNumberOfMessages},
		})
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "DOWN",
				"error":  "sqs queue unavailable: " + err.Error(),
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "UP"})
}

func (h *Handlers) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, metrics.Snapshot())
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func txToDTO(tx *domain.WagerTransaction) map[string]any {
	return map[string]any{
		"id":                             tx.ID(),
		"externalTransactionId":          tx.ExternalID(),
		"providerId":                     tx.ProviderID(),
		"idempotencyKey":                 tx.IdempotencyKey(),
		"walletId":                       tx.WalletID(),
		"playerId":                       tx.PlayerID(),
		"roundId":                        tx.RoundID(),
		"gameId":                         tx.GameID(),
		"kind":                           tx.Kind(),
		"amount":                         tx.Amount(),
		"referenceExternalTransactionId": tx.ReferenceExternalID(),
		"status":                         tx.Status(),
		"failureCode":                    tx.FailureCode(),
		"createdAt":                      tx.CreatedAt(),
		"updatedAt":                      tx.UpdatedAt(),
	}
}
