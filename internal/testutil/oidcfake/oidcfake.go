// Package oidcfake is a minimal OpenID Connect provider for tests. It serves
// discovery, JWKS and a token endpoint that enforces PKCE, and signs real
// RS256 ID tokens, so the production OIDC code runs unmodified against it.
package oidcfake

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// Identity is what the provider asserts about the user who logs in.
type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Groups        []string
}

// Provider is a running fake IdP.
type Provider struct {
	*httptest.Server
	ClientID string
	key      *rsa.PrivateKey

	mu      sync.Mutex
	pending map[string]grant // code -> grant
	// TamperNonce makes the next token carry a wrong nonce (replay test).
	TamperNonce bool
}

type grant struct {
	nonce, challenge string
	id               Identity
}

// New starts a provider; it is closed when the test ends.
func New(t *testing.T, clientID string) *Provider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &Provider{ClientID: clientID, key: key, pending: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("/jwks", p.jwks)
	mux.HandleFunc("/token", p.token)
	p.Server = httptest.NewServer(mux)
	t.Cleanup(p.Close)
	return p
}

// Authorize plays the user approving the login at the IdP: given the
// authorization URL LangDeck redirected to, it returns the code to send back.
func (p *Provider) Authorize(t *testing.T, authURL string, id Identity) (code, state string) {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("login must use PKCE S256: %s", authURL)
	}
	if q.Get("client_id") != p.ClientID || q.Get("response_type") != "code" {
		t.Fatalf("unexpected authorization request: %s", authURL)
	}
	code = "code-" + q.Get("state")
	p.mu.Lock()
	p.pending[code] = grant{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), id: id}
	p.mu.Unlock()
	return code, q.Get("state")
}

func (p *Provider) discovery(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                p.URL,
		"authorization_endpoint":                p.URL + "/authorize",
		"token_endpoint":                        p.URL + "/token",
		"jwks_uri":                              p.URL + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (p *Provider) jwks(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
		{Key: &p.key.PublicKey, KeyID: "k1", Algorithm: string(jose.RS256), Use: "sig"},
	}})
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	g, ok := p.pending[r.PostForm.Get("code")]
	delete(p.pending, r.PostForm.Get("code")) // codes are single use
	tamper := p.TamperNonce
	p.mu.Unlock()
	if !ok {
		writeErr(w, "invalid_grant")
		return
	}
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		writeErr(w, "invalid_grant") // PKCE verifier does not match the challenge
		return
	}
	nonce := g.nonce
	if tamper {
		nonce = "not-the-nonce"
	}
	now := time.Now()
	claims := map[string]any{
		"iss": p.URL, "aud": p.ClientID, "sub": g.id.Subject, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		"nonce": nonce, "email": g.id.Email, "email_verified": g.id.EmailVerified, "name": g.id.Name, "groups": g.id.Groups,
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: p.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	payload, _ := json.Marshal(claims)
	obj, err := signer.Sign(payload)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	idToken, _ := obj.CompactSerialize()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": idToken,
	})
}

func writeErr(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
