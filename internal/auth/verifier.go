// Package auth bridges Keycloak-issued access tokens to local application
// users. It validates bearer tokens against the realm's JWKS and provisions a
// matching local user the first time an authenticated caller is seen. No
// credentials or tokens are persisted — that is Keycloak's responsibility.
package auth

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Claims is the subset of token claims the app cares about.
type Claims struct {
	Subject           string
	PreferredUsername string
	Name              string
	Email             string
}

// TokenVerifier validates a raw bearer token and returns its claims. Declared
// as an interface so the middleware can be tested without a live IdP.
type TokenVerifier interface {
	Verify(ctx context.Context, rawToken string) (Claims, error)
}

// OIDCVerifier validates tokens against a Keycloak realm via OIDC discovery +
// JWKS. The provider is initialized lazily on first use (with retry) so the
// API can boot before Keycloak is reachable — only the first protected request
// pays the discovery cost.
type OIDCVerifier struct {
	issuerURL    string
	discoveryURL string
	audience     string

	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
}

// NewOIDCVerifier builds a verifier.
//   - issuerURL is the public realm URL that must match the token `iss`
//     (e.g. http://localhost:8081/realms/reccelog).
//   - discoveryURL is the URL the API uses to reach Keycloak for discovery/JWKS
//     (e.g. http://keycloak:8080/realms/reccelog on the shared docker network).
//     When empty it defaults to issuerURL.
//   - audience, when non-empty, is checked against the token `aud`; empty skips
//     the audience check.
func NewOIDCVerifier(issuerURL, discoveryURL, audience string) *OIDCVerifier {
	if discoveryURL == "" {
		discoveryURL = issuerURL
	}
	return &OIDCVerifier{issuerURL: issuerURL, discoveryURL: discoveryURL, audience: audience}
}

// ensure lazily builds the underlying verifier, retrying on every call until it
// succeeds (so a Keycloak that was down at boot recovers transparently).
func (v *OIDCVerifier) ensure() (*oidc.IDTokenVerifier, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.verifier != nil {
		return v.verifier, nil
	}

	// A transport-level timeout (not a cancelable context) bounds discovery and
	// JWKS fetches: go-oidc retains this context for the lifetime of the key
	// set, so it must not be one that gets canceled after the first request.
	httpClient := &http.Client{Timeout: 15 * time.Second}
	ctx := oidc.ClientContext(context.Background(), httpClient)
	if v.discoveryURL != v.issuerURL {
		// Discovery is fetched from discoveryURL but the issuer the tokens carry
		// is issuerURL — allow the mismatch explicitly.
		ctx = oidc.InsecureIssuerURLContext(ctx, v.issuerURL)
	}

	provider, err := oidc.NewProvider(ctx, v.discoveryURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}

	cfg := &oidc.Config{
		ClientID:             v.audience,
		SupportedSigningAlgs: []string{oidc.RS256},
	}
	if v.audience == "" {
		cfg.SkipClientIDCheck = true
	}
	v.verifier = provider.Verifier(cfg)
	return v.verifier, nil
}

// Verify validates the token signature, issuer, expiry and (optionally)
// audience, returning the claims the app needs.
func (v *OIDCVerifier) Verify(ctx context.Context, rawToken string) (Claims, error) {
	verifier, err := v.ensure()
	if err != nil {
		return Claims{}, err
	}

	tok, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		return Claims{}, fmt.Errorf("verify token: %w", err)
	}

	var c struct {
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
		Email             string `json:"email"`
	}
	if err := tok.Claims(&c); err != nil {
		return Claims{}, fmt.Errorf("decode claims: %w", err)
	}

	return Claims{
		Subject:           tok.Subject,
		PreferredUsername: c.PreferredUsername,
		Name:              c.Name,
		Email:             c.Email,
	}, nil
}
