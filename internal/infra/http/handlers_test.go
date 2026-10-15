package http_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	infraHttp "backend-challenge-go-jungle-gaming/internal/infra/http"
	"github.com/golang-jwt/jwt/v5"
)

func TestHealthLive(t *testing.T) {
	handlers := infraHttp.NewHandlers(nil, nil, "", nil, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	rec := httptest.NewRecorder()

	handlers.HandleHealthLive(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var res map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}

	if res["status"] != "UP" {
		t.Errorf("expected UP, got %s", res["status"])
	}
}

func TestWalletOperations_RestrictedToInternalService(t *testing.T) {
	handlers := infraHttp.NewHandlers(nil, nil, "", nil, nil, nil, nil, nil, nil)

	t.Run("external provider cannot create wallet", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewReader([]byte("{}")))
		ctx := context.WithValue(req.Context(), infraHttp.ProviderIDKey, "provider-a")
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handlers.HandleCreateWallet(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden, got %d", rec.Code)
		}
	})

	t.Run("external provider cannot get wallet", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/wallets/w-1", nil)
		ctx := context.WithValue(req.Context(), infraHttp.ProviderIDKey, "provider-a")
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handlers.HandleGetWallet(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden, got %d", rec.Code)
		}
	})

	t.Run("external provider cannot list ledger", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/wallets/w-1/ledger", nil)
		ctx := context.WithValue(req.Context(), infraHttp.ProviderIDKey, "provider-a")
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handlers.HandleListLedger(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden, got %d", rec.Code)
		}
	})

	t.Run("external provider cannot reconcile wallet", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/wallets/w-1/reconciliation", nil)
		ctx := context.WithValue(req.Context(), infraHttp.ProviderIDKey, "provider-a")
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handlers.HandleReconciliation(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden, got %d", rec.Code)
		}
	})
}

func TestAuthMiddleware_RealJWTValidation(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	middleware := infraHttp.NewAuthMiddleware("http://test-keycloak/realms/jungle-gaming")
	middleware.SetCustomKeyFunc(func(token *jwt.Token) (any, error) {
		return &privateKey.PublicKey, nil
	})

	nextCalled := false
	handler := middleware.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		prov, ok := infraHttp.GetAuthenticatedProvider(r.Context())
		if !ok || prov != "provider-a" {
			t.Errorf("expected authenticated provider-a, got %s", prov)
		}
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("valid token succeeds", func(t *testing.T) {
		nextCalled = false
		claims := jwt.MapClaims{
			"client_id": "provider-a",
			"iss":       "http://test-keycloak/realms/jungle-gaming",
			"exp":       time.Now().Add(time.Hour).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tokenString, err := token.SignedString(privateKey)
		if err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK || !nextCalled {
			t.Errorf("expected 200 OK, got %d", rec.Code)
		}
	})

	t.Run("expired token fails with 401", func(t *testing.T) {
		nextCalled = false
		claims := jwt.MapClaims{
			"client_id": "provider-a",
			"iss":       "http://test-keycloak/realms/jungle-gaming",
			"exp":       time.Now().Add(-time.Hour).Unix(), // expired!
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tokenString, err := token.SignedString(privateKey)
		if err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized || nextCalled {
			t.Errorf("expected 401 Unauthorized for expired token, got %d", rec.Code)
		}
	})

	t.Run("wrong signature fails with 401", func(t *testing.T) {
		nextCalled = false
		wrongKey, _ := rsa.GenerateKey(rand.Reader, 2048)
		claims := jwt.MapClaims{
			"client_id": "provider-a",
			"iss":       "http://test-keycloak/realms/jungle-gaming",
			"exp":       time.Now().Add(time.Hour).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tokenString, _ := token.SignedString(wrongKey)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized || nextCalled {
			t.Errorf("expected 401 Unauthorized for invalid signature, got %d", rec.Code)
		}
	})
}
