package googleauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

const (
	testAudience = "crosswalk-client.apps.googleusercontent.com"
	testEmail    = "person@example.edu"
	testDomain   = "example.edu"
	testKeyID    = "test-google-key"
)

type staticKeySetProvider struct {
	set jwk.Set
	err error
}

func (p staticKeySetProvider) KeySet(ctx context.Context) (jwk.Set, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return p.set, p.err
}

type signingFixture struct {
	private jwk.Key
	public  jwk.Set
}

func newSigningFixture(t *testing.T, keyID string) signingFixture {
	t.Helper()
	raw, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	private, err := jwk.Import(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := private.Set(jwk.KeyIDKey, keyID); err != nil {
		t.Fatal(err)
	}
	if err := private.Set(jwk.AlgorithmKey, jwa.RS256()); err != nil {
		t.Fatal(err)
	}
	public, err := jwk.PublicKeyOf(private)
	if err != nil {
		t.Fatal(err)
	}
	set := jwk.NewSet()
	if err := set.AddKey(public); err != nil {
		t.Fatal(err)
	}
	return signingFixture{private: private, public: set}
}

func (f signingFixture) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	token := jwt.New()
	for name, value := range claims {
		if err := token.Set(name, value); err != nil {
			t.Fatalf("setting %s: %v", name, err)
		}
	}
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.RS256(), f.private))
	if err != nil {
		t.Fatal(err)
	}
	return string(signed)
}

func validClaims(now time.Time) map[string]any {
	return map[string]any{
		jwt.AudienceKey:   []string{testAudience},
		jwt.ExpirationKey: now.Add(time.Hour),
		jwt.IssuedAtKey:   now.Add(-time.Minute),
		jwt.IssuerKey:     googleIssuer,
		jwt.SubjectKey:    "google-user-123",
		"email":           testEmail,
		"email_verified":  true,
		"hd":              testDomain,
	}
}

func copyClaims(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for name, value := range source {
		result[name] = value
	}
	return result
}

func testVerifier(t *testing.T, cfg Config, set jwk.Set) *Verifier {
	t.Helper()
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return newVerifierWithProvider(normalized, staticKeySetProvider{set: set})
}

func TestNormalizeConfigRequiresAudienceAndIdentityPolicy(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "missing audience", cfg: Config{HostedDomain: testDomain}},
		{name: "missing policy", cfg: Config{Audience: testAudience}},
		{name: "invalid domain", cfg: Config{Audience: testAudience, HostedDomain: "https://example.edu"}},
		{name: "invalid email", cfg: Config{Audience: testAudience, AllowedEmails: []string{"not-an-email"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeConfig(test.cfg)
			if !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("normalizeConfig() error = %v, want %v", err, ErrInvalidConfiguration)
			}
		})
	}
}

func TestVerifyAcceptsConfiguredHostedDomainOrEmail(t *testing.T) {
	keys := newSigningFixture(t, testKeyID)
	now := time.Now().UTC()

	tests := []struct {
		name   string
		cfg    Config
		mutate func(map[string]any)
	}{
		{
			name: "hosted domain",
			cfg:  Config{Audience: testAudience, HostedDomain: testDomain},
		},
		{
			name: "explicit email",
			cfg:  Config{Audience: testAudience, AllowedEmails: []string{" PERSON@example.edu "}},
			mutate: func(claims map[string]any) {
				delete(claims, "hd")
			},
		},
		{
			name: "legacy Google issuer",
			cfg:  Config{Audience: testAudience, HostedDomain: testDomain},
			mutate: func(claims map[string]any) {
				claims[jwt.IssuerKey] = googleLegacyIssuer
			},
		},
		{
			name: "email exception outside configured domain",
			cfg: Config{
				Audience:      testAudience,
				HostedDomain:  "other.example",
				AllowedEmails: []string{testEmail},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claims := copyClaims(validClaims(now))
			if test.mutate != nil {
				test.mutate(claims)
			}
			verifier := testVerifier(t, test.cfg, keys.public)
			if err := verifier.Verify(context.Background(), keys.sign(t, claims)); err != nil {
				t.Fatalf("Verify() error = %v", err)
			}
		})
	}
}

func TestVerifyRejectsInvalidSignatureClaimsAndIdentity(t *testing.T) {
	keys := newSigningFixture(t, testKeyID)
	wrongKeys := newSigningFixture(t, testKeyID)
	now := time.Now().UTC()
	config := Config{Audience: testAudience, HostedDomain: testDomain}

	tests := []struct {
		name   string
		signer signingFixture
		mutate func(map[string]any)
	}{
		{
			name:   "wrong signature",
			signer: wrongKeys,
		},
		{
			name:   "wrong issuer",
			signer: keys,
			mutate: func(claims map[string]any) { claims[jwt.IssuerKey] = "https://attacker.example" },
		},
		{
			name:   "issuer missing",
			signer: keys,
			mutate: func(claims map[string]any) { delete(claims, jwt.IssuerKey) },
		},
		{
			name:   "wrong audience",
			signer: keys,
			mutate: func(claims map[string]any) { claims[jwt.AudienceKey] = []string{"different-client"} },
		},
		{
			name:   "missing audience",
			signer: keys,
			mutate: func(claims map[string]any) { delete(claims, jwt.AudienceKey) },
		},
		{
			name:   "expired",
			signer: keys,
			mutate: func(claims map[string]any) { claims[jwt.ExpirationKey] = now.Add(-time.Second) },
		},
		{
			name:   "missing expiration",
			signer: keys,
			mutate: func(claims map[string]any) { delete(claims, jwt.ExpirationKey) },
		},
		{
			name:   "issued at is in future",
			signer: keys,
			mutate: func(claims map[string]any) { claims[jwt.IssuedAtKey] = now.Add(time.Hour) },
		},
		{
			name:   "issued at missing",
			signer: keys,
			mutate: func(claims map[string]any) { delete(claims, jwt.IssuedAtKey) },
		},
		{
			name:   "subject missing",
			signer: keys,
			mutate: func(claims map[string]any) { delete(claims, jwt.SubjectKey) },
		},
		{
			name:   "not before is in future",
			signer: keys,
			mutate: func(claims map[string]any) { claims[jwt.NotBeforeKey] = now.Add(time.Hour) },
		},
		{
			name:   "email is not verified",
			signer: keys,
			mutate: func(claims map[string]any) { claims["email_verified"] = false },
		},
		{
			name:   "email verification missing",
			signer: keys,
			mutate: func(claims map[string]any) { delete(claims, "email_verified") },
		},
		{
			name:   "email verification has wrong type",
			signer: keys,
			mutate: func(claims map[string]any) { claims["email_verified"] = "true" },
		},
		{
			name:   "email missing",
			signer: keys,
			mutate: func(claims map[string]any) { delete(claims, "email") },
		},
		{
			name:   "hosted domain missing",
			signer: keys,
			mutate: func(claims map[string]any) { delete(claims, "hd") },
		},
		{
			name:   "hosted domain mismatch",
			signer: keys,
			mutate: func(claims map[string]any) { claims["hd"] = "attacker.example" },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claims := copyClaims(validClaims(now))
			if test.mutate != nil {
				test.mutate(claims)
			}
			verifier := testVerifier(t, config, keys.public)
			err := verifier.Verify(context.Background(), test.signer.sign(t, claims))
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Verify() error = %v, want %v", err, ErrInvalidToken)
			}
		})
	}
}

func TestVerifyRejectsEmailOutsideExplicitAllowlist(t *testing.T) {
	keys := newSigningFixture(t, testKeyID)
	verifier := testVerifier(t, Config{
		Audience:      testAudience,
		AllowedEmails: []string{"allowed@example.edu"},
	}, keys.public)
	err := verifier.Verify(context.Background(), keys.sign(t, validClaims(time.Now().UTC())))
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Verify() error = %v, want %v", err, ErrInvalidToken)
	}
}

func TestVerifyHonorsCanceledContext(t *testing.T) {
	keys := newSigningFixture(t, testKeyID)
	verifier := testVerifier(t, Config{Audience: testAudience, HostedDomain: testDomain}, keys.public)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifier.Verify(ctx, keys.sign(t, validClaims(time.Now().UTC()))); !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify() error = %v, want %v", err, context.Canceled)
	}
}

func TestVerifierCachesSigningKeys(t *testing.T) {
	keys := newSigningFixture(t, testKeyID)
	jwks, err := json.Marshal(keys.public)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(jwks)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	verifier, err := newVerifier(ctx,
		Config{Audience: testAudience, HostedDomain: testDomain},
		dependencies{jwksURL: server.URL, httpClient: server.Client()},
	)
	if err != nil {
		t.Fatal(err)
	}
	token := keys.sign(t, validClaims(time.Now().UTC()))
	const verificationCount = 16
	errorsFound := make(chan error, verificationCount)
	var wait sync.WaitGroup
	for range verificationCount {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := verifier.Verify(ctx, token); err != nil {
				errorsFound <- err
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("JWKS requests = %d, want 1", got)
	}
}
