// Package googleauth verifies Google OpenID Connect identity tokens for the
// Crosswalk HTTP service.
package googleauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

const (
	googleJWKSURL      = "https://www.googleapis.com/oauth2/v3/certs"
	googleIssuer       = "https://accounts.google.com"
	googleLegacyIssuer = "accounts.google.com"
	maxIdentityToken   = 64 << 10
	maxJWKSBytes       = 1 << 20
	minJWKSRefresh     = 5 * time.Minute
	maxJWKSRefresh     = 12 * time.Hour
	defaultHTTPTimeout = 5 * time.Second
)

var (
	// ErrInvalidConfiguration indicates that Google authentication was enabled
	// without a complete, restrictive identity policy.
	ErrInvalidConfiguration = errors.New("invalid Google authentication configuration")
	// ErrInvalidToken indicates that an identity token failed cryptographic,
	// registered-claim, or identity-policy validation.
	ErrInvalidToken = errors.New("invalid Google identity token")
)

// Config defines which Google identity tokens Crosswalk accepts. Audience is
// mandatory. At least one of HostedDomain or AllowedEmails must be configured;
// when both are present, matching either policy grants access.
type Config struct {
	Audience      string
	HostedDomain  string
	AllowedEmails []string
}

type normalizedConfig struct {
	audience      string
	hostedDomain  string
	allowedEmails map[string]struct{}
}

// Verifier validates Google signatures and claims against a restrictive
// deployment policy. It is safe for concurrent use.
type Verifier struct {
	config normalizedConfig
	keys   keySetProvider
}

type keySetProvider interface {
	KeySet(context.Context) (jwk.Set, error)
}

type cacheProvider struct {
	cache *jwk.Cache
	url   string
}

func (p cacheProvider) KeySet(ctx context.Context) (jwk.Set, error) {
	return p.cache.Lookup(ctx, p.url)
}

type dependencies struct {
	jwksURL    string
	httpClient *http.Client
}

// New constructs a verifier and primes its cached Google signing-key set.
// The supplied context owns the cache lifecycle and can cancel the initial
// network fetch.
func New(ctx context.Context, cfg Config) (*Verifier, error) {
	client := jwk.WrapHTTPClientDefaults(&http.Client{
		Timeout: defaultHTTPTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			// The Google JWKS endpoint is fixed. Following redirects would add an
			// unnecessary network trust boundary to authentication startup.
			return http.ErrUseLastResponse
		},
	})
	return newVerifier(ctx, cfg, dependencies{
		jwksURL:    googleJWKSURL,
		httpClient: client,
	})
}

func newVerifier(ctx context.Context, cfg Config, deps dependencies) (*Verifier, error) {
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, fmt.Errorf("%w: context is required", ErrInvalidConfiguration)
	}
	if strings.TrimSpace(deps.jwksURL) == "" || deps.httpClient == nil {
		return nil, fmt.Errorf("%w: signing-key source is required", ErrInvalidConfiguration)
	}

	cache, err := jwk.NewCache(ctx, httprc.NewClient())
	if err != nil {
		return nil, fmt.Errorf("creating Google signing-key cache: %w", err)
	}
	if err := cache.Register(ctx, deps.jwksURL,
		jwk.WithHTTPClient(deps.httpClient),
		jwk.WithMaxFetchBodySize(maxJWKSBytes),
		jwk.WithRejectDuplicateKID(true),
		jwk.WithMinInterval(minJWKSRefresh),
		jwk.WithMaxInterval(maxJWKSRefresh),
	); err != nil {
		return nil, fmt.Errorf("initializing Google signing-key cache: %w", err)
	}

	return newVerifierWithProvider(normalized, cacheProvider{cache: cache, url: deps.jwksURL}), nil
}

func newVerifierWithProvider(cfg normalizedConfig, keys keySetProvider) *Verifier {
	return &Verifier{config: cfg, keys: keys}
}

// Verify checks the token signature, Google issuer, configured audience,
// expiration, optional not-before time, verified email, and deployment identity
// policy. It never calls Google's tokeninfo endpoint.
func (v *Verifier) Verify(ctx context.Context, token string) error {
	if v == nil || v.keys == nil {
		return fmt.Errorf("%w: verifier is not initialized", ErrInvalidToken)
	}
	if ctx == nil {
		return fmt.Errorf("%w: context is required", ErrInvalidToken)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if token == "" || len(token) > maxIdentityToken {
		return fmt.Errorf("%w: token size is invalid", ErrInvalidToken)
	}

	keySet, err := v.keys.KeySet(ctx)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return fmt.Errorf("%w: signing keys are unavailable", ErrInvalidToken)
	}
	parsed, err := jwt.Parse([]byte(token),
		jwt.WithKeySet(keySet),
		jwt.WithValidate(true),
		jwt.WithContext(ctx),
		jwt.WithAudience(v.config.audience),
		jwt.WithRequiredClaim(jwt.AudienceKey),
		jwt.WithRequiredClaim(jwt.ExpirationKey),
		jwt.WithRequiredClaim(jwt.IssuedAtKey),
		jwt.WithRequiredClaim(jwt.IssuerKey),
		jwt.WithRequiredClaim(jwt.SubjectKey),
		jwt.WithRequiredClaim("email"),
		jwt.WithRequiredClaim("email_verified"),
		jwt.WithValidator(jwt.ValidatorFunc(validateGoogleIssuer)),
	)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return fmt.Errorf("%w: signature or registered claims failed", ErrInvalidToken)
	}

	var emailVerified bool
	if err := parsed.Get("email_verified", &emailVerified); err != nil || !emailVerified {
		return fmt.Errorf("%w: email is not verified", ErrInvalidToken)
	}
	var email string
	if err := parsed.Get("email", &email); err != nil {
		return fmt.Errorf("%w: email claim is invalid", ErrInvalidToken)
	}
	email = normalizeEmail(email)
	if email == "" {
		return fmt.Errorf("%w: email claim is empty", ErrInvalidToken)
	}

	_, emailAllowed := v.config.allowedEmails[email]
	domainAllowed := false
	if v.config.hostedDomain != "" {
		var hostedDomain string
		if err := parsed.Get("hd", &hostedDomain); err == nil {
			domainAllowed = strings.EqualFold(strings.TrimSpace(hostedDomain), v.config.hostedDomain)
		}
	}
	if !emailAllowed && !domainAllowed {
		return fmt.Errorf("%w: identity is not allowed", ErrInvalidToken)
	}
	return nil
}

func validateGoogleIssuer(_ context.Context, token jwt.Token) error {
	issuer, ok := token.Issuer()
	if !ok || (issuer != googleIssuer && issuer != googleLegacyIssuer) {
		return errors.New("issuer is not Google")
	}
	return nil
}

func normalizeConfig(cfg Config) (normalizedConfig, error) {
	audience := strings.TrimSpace(cfg.Audience)
	if audience == "" {
		return normalizedConfig{}, fmt.Errorf("%w: audience is required", ErrInvalidConfiguration)
	}
	hostedDomain := strings.ToLower(strings.TrimSpace(cfg.HostedDomain))
	if hostedDomain != "" && !validDomain(hostedDomain) {
		return normalizedConfig{}, fmt.Errorf("%w: hosted domain %q is invalid", ErrInvalidConfiguration, cfg.HostedDomain)
	}

	allowedEmails := make(map[string]struct{}, len(cfg.AllowedEmails))
	for _, configured := range cfg.AllowedEmails {
		email := normalizeEmail(configured)
		if !validEmail(email) {
			return normalizedConfig{}, fmt.Errorf("%w: allowed email %q is invalid", ErrInvalidConfiguration, configured)
		}
		allowedEmails[email] = struct{}{}
	}
	if hostedDomain == "" && len(allowedEmails) == 0 {
		return normalizedConfig{}, fmt.Errorf("%w: hosted domain or allowed email is required", ErrInvalidConfiguration)
	}

	return normalizedConfig{
		audience:      audience,
		hostedDomain:  hostedDomain,
		allowedEmails: allowedEmails,
	}, nil
}

func normalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validEmail(value string) bool {
	if value == "" || len(value) > 254 || strings.Count(value, "@") != 1 || strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	local, domain, _ := strings.Cut(value, "@")
	return local != "" && validDomain(domain)
}

func validDomain(value string) bool {
	if value == "" || len(value) > 253 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}
