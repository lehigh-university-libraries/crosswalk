package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lehigh-university-libraries/crosswalk/server/googleauth"
	"github.com/lehigh-university-libraries/crosswalk/server/httpapi"
	drupalsource "github.com/lehigh-university-libraries/crosswalk/source/drupal"
	"github.com/spf13/cobra"
)

const (
	googleAudienceEnv      = "CROSSWALK_GOOGLE_AUDIENCE"
	googleHostedDomainEnv  = "CROSSWALK_GOOGLE_HOSTED_DOMAIN"
	googleAllowedEmailsEnv = "CROSSWALK_GOOGLE_ALLOWED_EMAILS"
)

type serveOptions struct {
	address             string
	sharedSecretEnv     string
	maxBodyBytes        int64
	maxOutputBytes      int64
	maxRows             int
	maxCells            int64
	maxConcurrent       int
	requestTimeout      time.Duration
	readHeaderTimeout   time.Duration
	readTimeout         time.Duration
	writeTimeout        time.Duration
	idleTimeout         time.Duration
	shutdownTimeout     time.Duration
	maxHeaderBytes      int
	specPath            string
	googleAudience      string
	googleHostedDomain  string
	googleAllowedEmails string
	drupalJSONAPI       string
	drupalProfile       string
	drupalTokenEnv      string
	drupalUsernameEnv   string
	drupalPasswordEnv   string
	allowPublicHTTP     bool
}

type googleVerifierFactory func(context.Context, googleauth.Config) (httpapi.BearerVerifier, error)

func newServeCmd() *cobra.Command {
	options := &serveOptions{
		address:           "127.0.0.1:8080",
		sharedSecretEnv:   "SHARED_SECRET",
		maxBodyBytes:      16 << 20,
		maxOutputBytes:    64 << 20,
		maxRows:           100_000,
		maxCells:          1_000_000,
		maxConcurrent:     8,
		requestTimeout:    90 * time.Second,
		readHeaderTimeout: 5 * time.Second,
		readTimeout:       30 * time.Second,
		writeTimeout:      2 * time.Minute,
		idleTimeout:       time.Minute,
		shutdownTimeout:   10 * time.Second,
		maxHeaderBytes:    1 << 20,
	}

	command := &cobra.Command{
		Use:   "serve",
		Short: "Serve metadata validation and Workbench transformation over HTTP",
		Long: `Serve the Crosswalk metadata engine over HTTP.

The Workbench endpoints require either X-Secret authentication using a secret
read from the configured environment variable, or a verified Google identity
token from an allowed Workspace domain or email address. Startup fails when no
complete authentication mechanism is configured. Transformations are
side-effect-free. When a Drupal JSON:API endpoint is configured, the matches
endpoint performs bounded read-only duplicate lookup; no endpoint mutates Drupal
or writes shared temporary files.`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return options.run(command)
		},
		SilenceUsage: true,
	}
	flags := command.Flags()
	flags.StringVar(&options.address, "address", options.address, "HTTP listen address")
	flags.BoolVar(&options.allowPublicHTTP, "allow-public-http", false, "explicitly allow plaintext HTTP on a non-loopback address (use only behind a trusted TLS proxy)")
	flags.StringVar(&options.sharedSecretEnv, "shared-secret-env", options.sharedSecretEnv, "environment variable containing an optional shared secret")
	flags.StringVar(&options.googleAudience, "google-audience", "", "accepted Google token audience (or "+googleAudienceEnv+")")
	flags.StringVar(&options.googleHostedDomain, "google-hosted-domain", "", "allowed Google Workspace hosted domain (or "+googleHostedDomainEnv+")")
	flags.StringVar(&options.googleAllowedEmails, "google-allowed-emails", "", "comma-separated allowed Google emails (or "+googleAllowedEmailsEnv+")")
	flags.Int64Var(&options.maxBodyBytes, "max-body-bytes", options.maxBodyBytes, "maximum request body size in bytes")
	flags.Int64Var(&options.maxOutputBytes, "max-output-bytes", options.maxOutputBytes, "maximum generated response size in bytes")
	flags.IntVar(&options.maxRows, "max-rows", options.maxRows, "maximum decoded rows per request")
	flags.Int64Var(&options.maxCells, "max-cells", options.maxCells, "maximum decoded cells per request")
	flags.IntVar(&options.maxConcurrent, "max-concurrent-requests", options.maxConcurrent, "maximum concurrent metadata requests")
	flags.DurationVar(&options.requestTimeout, "request-timeout", options.requestTimeout, "maximum metadata processing time per request")
	flags.DurationVar(&options.readHeaderTimeout, "read-header-timeout", options.readHeaderTimeout, "maximum time to read request headers")
	flags.DurationVar(&options.readTimeout, "read-timeout", options.readTimeout, "maximum time to read a complete request")
	flags.DurationVar(&options.writeTimeout, "write-timeout", options.writeTimeout, "maximum time to write a response")
	flags.DurationVar(&options.idleTimeout, "idle-timeout", options.idleTimeout, "maximum keep-alive idle time")
	flags.DurationVar(&options.shutdownTimeout, "shutdown-timeout", options.shutdownTimeout, "maximum graceful shutdown time")
	flags.IntVar(&options.maxHeaderBytes, "max-header-bytes", options.maxHeaderBytes, "maximum request header size in bytes")
	flags.StringVar(&options.specPath, "spec", "", "transformation specification JSON/YAML (default built-in Fabricator contract)")
	flags.StringVar(&options.drupalJSONAPI, "drupal-jsonapi", "", "Drupal JSON:API root used for read-only existing-item lookup")
	flags.StringVar(&options.drupalProfile, "drupal-profile", "", "stored Drupal profile defining repository fields and existing-item policy")
	flags.StringVar(&options.drupalTokenEnv, "drupal-token-env", "DRUPAL_JSONAPI_TOKEN", "environment variable containing an optional Drupal bearer token")
	flags.StringVar(&options.drupalUsernameEnv, "drupal-username-env", "DRUPAL_JSONAPI_USERNAME", "environment variable containing an optional Drupal Basic username")
	flags.StringVar(&options.drupalPasswordEnv, "drupal-password-env", "DRUPAL_JSONAPI_PASSWORD", "environment variable containing the Drupal Basic password")
	return command
}

func (o *serveOptions) run(command *cobra.Command) error {
	if err := o.validate(); err != nil {
		return err
	}
	var systemProfile *drupalReconciliationProfile
	if strings.TrimSpace(o.drupalProfile) != "" {
		var err error
		systemProfile, err = loadDrupalReconciliationProfile(o.drupalProfile, strings.TrimSpace(o.drupalJSONAPI) != "")
		if err != nil {
			return err
		}
	}

	ctx, stop := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	secret, bearerVerifier, err := o.authentication(ctx, func(ctx context.Context, cfg googleauth.Config) (httpapi.BearerVerifier, error) {
		return googleauth.New(ctx, cfg)
	})
	if err != nil {
		return err
	}

	engine := httpapi.NewCrosswalkEngine()
	if systemProfile != nil {
		engine, err = httpapi.NewCrosswalkEngineWithSpec(systemProfile.transformation)
		if err != nil {
			return fmt.Errorf("configuring profile-bound transformation: %w", err)
		}
	}
	if o.specPath != "" {
		transformation, loadErr := loadTransformationSpec(o.specPath, "csv", "islandora-workbench")
		if loadErr != nil {
			return loadErr
		}
		if transformation.Fingerprint.Profile != "" && systemProfile == nil {
			return fmt.Errorf("profile-bound --spec requires the exact --drupal-profile")
		}
		if transformation.Fingerprint.Profile == "" && systemProfile != nil {
			return fmt.Errorf("unbound --spec cannot be combined with --drupal-profile")
		}
		if systemProfile != nil {
			if transformation.Fingerprint.Model != systemProfile.compiled.ModelFingerprint() {
				return fmt.Errorf("transformation model fingerprint does not match Drupal profile model")
			}
			if transformation.Fingerprint.Profile != systemProfile.compiled.Fingerprint() {
				return fmt.Errorf("transformation profile fingerprint does not match Drupal profile")
			}
		}
		engine, err = httpapi.NewCrosswalkEngineWithSpec(transformation)
		if err != nil {
			return err
		}
	}
	var finder *drupalsource.Client
	if strings.TrimSpace(o.drupalJSONAPI) != "" {
		finder = drupalsource.NewClient(o.drupalJSONAPI)
		finder.SystemProfile = systemProfile.compiled
		token := strings.TrimSpace(os.Getenv(o.drupalTokenEnv))
		username := strings.TrimSpace(os.Getenv(o.drupalUsernameEnv))
		password := os.Getenv(o.drupalPasswordEnv)
		switch {
		case token != "" && username != "":
			return fmt.Errorf("configure either Drupal bearer token or Basic credentials, not both")
		case token != "":
			finder.Auth = drupalsource.BearerTokenAuth(token)
		case username != "":
			finder.Auth = drupalsource.BasicAuth{Username: username, Password: password}
		case password != "":
			return fmt.Errorf("drupal password is configured without a username")
		}
	}
	if systemProfile != nil {
		if err := engine.ConfigureReconciliation(httpapi.ReconciliationConfig{
			Finder: finder, Policy: systemProfile.policy, Provenance: systemProfile.provenance, TargetProfile: systemProfile.compiled,
		}); err != nil {
			return fmt.Errorf("configuring profile-aware reconciliation: %w", err)
		}
	}

	handler, err := httpapi.New(engine, httpapi.Config{
		SharedSecret:          secret,
		BearerVerifier:        bearerVerifier,
		MaxBodyBytes:          o.maxBodyBytes,
		MaxOutputBytes:        o.maxOutputBytes,
		MaxRows:               o.maxRows,
		MaxCells:              o.maxCells,
		MaxConcurrentRequests: o.maxConcurrent,
		RequestTimeout:        o.requestTimeout,
	})
	if err != nil {
		return fmt.Errorf("configuring HTTP API: %w", err)
	}

	listener, err := net.Listen("tcp", o.address)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", o.address, err)
	}
	defer listener.Close()

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: o.readHeaderTimeout,
		ReadTimeout:       o.readTimeout,
		WriteTimeout:      o.writeTimeout,
		IdleTimeout:       o.idleTimeout,
		MaxHeaderBytes:    o.maxHeaderBytes,
	}

	serveError := make(chan error, 1)
	go func() {
		serveError <- server.Serve(listener)
	}()

	slog.Info("Crosswalk HTTP server started", "address", listener.Addr().String())
	select {
	case err := <-serveError:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serving HTTP: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), o.shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		_ = server.Close()
		return fmt.Errorf("shutting down HTTP server: %w", err)
	}
	if err := <-serveError; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serving HTTP: %w", err)
	}
	return nil
}

func (o *serveOptions) authentication(ctx context.Context, factory googleVerifierFactory) (string, httpapi.BearerVerifier, error) {
	secret, _ := os.LookupEnv(o.sharedSecretEnv)
	if strings.TrimSpace(secret) == "" {
		secret = ""
	}

	audience := optionOrEnvironment(o.googleAudience, googleAudienceEnv)
	hostedDomain := optionOrEnvironment(o.googleHostedDomain, googleHostedDomainEnv)
	allowedEmailValues := optionOrEnvironment(o.googleAllowedEmails, googleAllowedEmailsEnv)
	googleConfigured := audience != "" || hostedDomain != "" || allowedEmailValues != ""

	var verifier httpapi.BearerVerifier
	if googleConfigured {
		if factory == nil {
			return "", nil, errors.New("google verifier factory is required")
		}
		var err error
		verifier, err = factory(ctx, googleauth.Config{
			Audience:      audience,
			HostedDomain:  hostedDomain,
			AllowedEmails: commaSeparatedValues(allowedEmailValues),
		})
		if err != nil {
			return "", nil, fmt.Errorf("configuring Google authentication: %w", err)
		}
		if verifier == nil {
			return "", nil, errors.New("configuring Google authentication: verifier is unavailable")
		}
	}
	if secret == "" && verifier == nil {
		return "", nil, fmt.Errorf("authentication is not configured: shared secret environment variable %s is missing or blank and Google authentication is disabled", o.sharedSecretEnv)
	}
	return secret, verifier, nil
}

func optionOrEnvironment(optionValue, environmentName string) string {
	if value := strings.TrimSpace(optionValue); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv(environmentName))
}

func commaSeparatedValues(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func (o *serveOptions) validate() error {
	if strings.TrimSpace(o.address) == "" {
		return errors.New("HTTP listen address is required")
	}
	if strings.TrimSpace(o.sharedSecretEnv) == "" {
		return errors.New("shared secret environment variable name is required")
	}
	if !o.allowPublicHTTP {
		loopback, err := loopbackListenAddress(o.address)
		if err != nil {
			return err
		}
		if !loopback {
			return errors.New("refusing to expose credential-bearing plaintext HTTP on a non-loopback address; terminate TLS at a trusted local proxy and pass --allow-public-http explicitly")
		}
	}
	if o.maxBodyBytes <= 0 {
		return errors.New("maximum body size must be positive")
	}
	if o.maxOutputBytes <= 0 {
		return errors.New("maximum output size must be positive")
	}
	if o.maxRows <= 0 {
		return errors.New("maximum row count must be positive")
	}
	if o.maxCells <= 0 {
		return errors.New("maximum cell count must be positive")
	}
	if o.maxConcurrent <= 0 {
		return errors.New("maximum concurrent request count must be positive")
	}
	if o.maxHeaderBytes <= 0 {
		return errors.New("maximum header size must be positive")
	}
	if strings.TrimSpace(o.drupalJSONAPI) != "" && strings.TrimSpace(o.drupalProfile) == "" {
		return errors.New("--drupal-profile is required with --drupal-jsonapi")
	}
	for name, value := range map[string]time.Duration{
		"request-timeout":     o.requestTimeout,
		"read-header-timeout": o.readHeaderTimeout,
		"read-timeout":        o.readTimeout,
		"write-timeout":       o.writeTimeout,
		"idle-timeout":        o.idleTimeout,
		"shutdown-timeout":    o.shutdownTimeout,
	} {
		if value <= 0 {
			return fmt.Errorf("%s must be positive", name)
		}
	}
	return nil
}

func loopbackListenAddress(address string) (bool, error) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return false, fmt.Errorf("invalid HTTP listen address %q: %w", address, err)
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true, nil
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback(), nil
}
