package http

import (
	"net/http"
)

func NewRouter(handlers *Handlers, authMiddleware *AuthMiddleware) http.Handler {
	mux := http.NewServeMux()

	// Public health checks and metrics
	mux.HandleFunc("GET /health/live", handlers.HandleHealthLive)
	mux.HandleFunc("GET /health/ready", handlers.HandleHealthReady)
	mux.HandleFunc("GET /metrics", handlers.HandleMetrics)

	// Protected routes
	authMux := http.NewServeMux()

	// Wallets
	authMux.HandleFunc("POST /wallets", handlers.HandleCreateWallet)
	authMux.HandleFunc("GET /wallets/{walletId}", handlers.HandleGetWallet)
	authMux.HandleFunc("GET /wallets/{walletId}/ledger", handlers.HandleListLedger)
	authMux.HandleFunc("POST /wallets/{walletId}/reconciliation", handlers.HandleReconciliation)

	// Wagering
	authMux.HandleFunc("POST /wagering/transactions", handlers.HandleProcessWager)
	authMux.HandleFunc("GET /wagering/transactions/{transactionId}", handlers.HandleGetTransaction)
	authMux.HandleFunc("GET /providers/{providerId}/wagering/transactions/{externalTransactionId}", handlers.HandleGetTransactionByProviderAndExternalID)

	protectedHandler := authMiddleware.Authenticate(authMux)

	mux.Handle("/", protectedHandler)
	return mux
}
