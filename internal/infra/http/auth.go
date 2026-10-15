package http

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	ProviderIDKey contextKey = "providerId"
	IsInternalKey contextKey = "isInternal"
)

func GetAuthenticatedProvider(ctx context.Context) (string, bool) {
	providerID, ok := ctx.Value(ProviderIDKey).(string)
	return providerID, ok
}

func IsInternalCaller(ctx context.Context) bool {
	val, ok := ctx.Value(IsInternalKey).(bool)
	return ok && val
}

type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	KID string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type AuthMiddleware struct {
	issuerURL     string
	httpClient    *http.Client
	cachedKeys    map[string]*rsa.PublicKey
	cacheMutex    sync.RWMutex
	customKeyFunc jwt.Keyfunc // For tests or alternative verifiers
}

func NewAuthMiddleware(issuerURL string) *AuthMiddleware {
	return &AuthMiddleware{
		issuerURL:  strings.TrimRight(issuerURL, "/"),
		httpClient: &http.Client{Timeout: 5 * time.Second},
		cachedKeys: make(map[string]*rsa.PublicKey),
	}
}

func (m *AuthMiddleware) SetCustomKeyFunc(kf jwt.Keyfunc) {
	m.customKeyFunc = kf
}

func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeJSONError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeJSONError(w, http.StatusUnauthorized, "invalid authorization header format")
			return
		}

		tokenString := parts[1]

		keyFunc := m.customKeyFunc
		if keyFunc == nil {
			keyFunc = m.getKey
		}

		parserOpts := []jwt.ParserOption{
			jwt.WithExpirationRequired(),
		}

		token, err := jwt.Parse(tokenString, keyFunc, parserOpts...)
		if err != nil || !token.Valid {
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "invalid token claims")
			return
		}

		// Validate issuer
		if m.issuerURL != "" && m.customKeyFunc == nil {
			tokenIss, _ := claims["iss"].(string)
			if tokenIss == "" {
				writeJSONError(w, http.StatusUnauthorized, "missing token issuer")
				return
			}
			expectedRealm := m.issuerURL
			if idx := strings.Index(m.issuerURL, "/realms/"); idx != -1 {
				expectedRealm = m.issuerURL[idx:]
			}
			if tokenIss != m.issuerURL && !strings.HasSuffix(tokenIss, expectedRealm) {
				writeJSONError(w, http.StatusUnauthorized, "invalid token issuer")
				return
			}
		}

		var providerID string
		if clientID, ok := claims["client_id"].(string); ok && clientID != "" {
			providerID = clientID
		} else if azp, ok := claims["azp"].(string); ok && azp != "" {
			providerID = azp
		} else if sub, ok := claims["sub"].(string); ok && sub != "" {
			providerID = sub
		}

		if providerID == "" {
			writeJSONError(w, http.StatusUnauthorized, "could not determine provider identity from token")
			return
		}

		ctx := context.WithValue(r.Context(), ProviderIDKey, providerID)
		if strings.EqualFold(providerID, "internal") {
			ctx = context.WithValue(ctx, IsInternalKey, true)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *AuthMiddleware) getKey(token *jwt.Token) (any, error) {
	if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
		return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
	}

	kid, ok := token.Header["kid"].(string)
	if !ok || kid == "" {
		return nil, errors.New("missing kid in token header")
	}

	m.cacheMutex.RLock()
	key, found := m.cachedKeys[kid]
	m.cacheMutex.RUnlock()
	if found {
		return key, nil
	}

	// Refresh JWKS from Keycloak
	if err := m.refreshJWKS(); err != nil {
		return nil, fmt.Errorf("failed to refresh jwks from %s: %w", m.issuerURL, err)
	}

	m.cacheMutex.RLock()
	key, found = m.cachedKeys[kid]
	m.cacheMutex.RUnlock()
	if !found {
		return nil, fmt.Errorf("key id %s not found in jwks", kid)
	}

	return key, nil
}

func (m *AuthMiddleware) refreshJWKS() error {
	if m.issuerURL == "" {
		return errors.New("issuer url is empty")
	}

	jwksURL := fmt.Sprintf("%s/protocol/openid-connect/certs", m.issuerURL)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, jwksURL, nil)
	if err != nil {
		return err
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks endpoint returned status %d", resp.StatusCode)
	}

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return err
	}

	newKeys := make(map[string]*rsa.PublicKey)
	for _, k := range jwks.Keys {
		if k.Kty == "RSA" && k.N != "" && k.E != "" {
			pubKey, err := parseRSAPublicKey(k.N, k.E)
			if err == nil {
				newKeys[k.KID] = pubKey
			}
		}
	}

	m.cacheMutex.Lock()
	m.cachedKeys = newKeys
	m.cacheMutex.Unlock()

	return nil
}

func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(nStr, "="))
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(eStr, "="))
	if err != nil {
		return nil, err
	}
	var eInt int
	for _, b := range eBytes {
		eInt = (eInt << 8) | int(b)
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: eInt,
	}, nil
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
