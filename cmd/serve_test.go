package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lehigh-university-libraries/crosswalk/server/googleauth"
	"github.com/lehigh-university-libraries/crosswalk/server/httpapi"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

type commandVerifier struct{}

func (commandVerifier) Verify(context.Context, string) error { return nil }

func clearGoogleAuthEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{googleAudienceEnv, googleHostedDomainEnv, googleAllowedEmailsEnv} {
		t.Setenv(name, "")
	}
}

func TestServeCommandExposesOperationalLimits(t *testing.T) {
	command := newServeCmd()
	for _, name := range []string{
		"address",
		"allow-public-http",
		"shared-secret-env",
		"google-audience",
		"google-hosted-domain",
		"google-allowed-emails",
		"max-body-bytes",
		"max-output-bytes",
		"max-rows",
		"max-cells",
		"max-concurrent-requests",
		"request-timeout",
		"read-header-timeout",
		"read-timeout",
		"write-timeout",
		"idle-timeout",
		"shutdown-timeout",
		"max-header-bytes",
		"spec",
		"drupal-jsonapi",
		"drupal-profile",
		"drupal-token-env",
		"drupal-username-env",
		"drupal-password-env",
	} {
		if command.Flags().Lookup(name) == nil {
			t.Errorf("serve command is missing --%s", name)
		}
	}
}

func TestServeRequiresCanonicalProfileForDrupalLookup(t *testing.T) {
	t.Parallel()
	options := serveOptions{
		address: "127.0.0.1:8080", sharedSecretEnv: "SHARED_SECRET", maxBodyBytes: 1,
		maxOutputBytes: 1, maxRows: 1, maxCells: 1, maxConcurrent: 1,
		requestTimeout: time.Second, readHeaderTimeout: time.Second, readTimeout: time.Second,
		writeTimeout: time.Second, idleTimeout: time.Second, shutdownTimeout: time.Second,
		maxHeaderBytes: 1, drupalJSONAPI: "https://repository.example.edu/jsonapi",
	}
	if err := options.validate(); err == nil || !strings.Contains(err.Error(), "--drupal-profile is required") {
		t.Fatalf("validate() error = %v", err)
	}
}

func TestServeRejectsFingerprintDriftBeforeListening(t *testing.T) {
	clearGoogleAuthEnvironment(t)
	const environmentName = "CROSSWALK_TEST_SPEC_SECRET"
	t.Setenv(environmentName, "test-secret")
	transformation := spec.FabricatorWorkbench()
	transformation.Fingerprint.Value = strings.Repeat("0", 64)
	data, err := json.Marshal(transformation)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "drift.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	command := newServeCmd()
	command.SetArgs([]string{"--shared-secret-env", environmentName, "--address", "127.0.0.1:0", "--spec", path})
	err = command.Execute()
	if err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") {
		t.Fatalf("serve error = %v, want fingerprint mismatch", err)
	}
}

func TestServeFailsBeforeListeningWhenSecretIsMissingOrBlank(t *testing.T) {
	clearGoogleAuthEnvironment(t)
	const environmentName = "CROSSWALK_TEST_MISSING_SECRET"
	for _, value := range []string{"", "   "} {
		t.Run("value="+value, func(t *testing.T) {
			t.Setenv(environmentName, value)
			command := newServeCmd()
			command.SetArgs([]string{"--shared-secret-env", environmentName, "--address", "127.0.0.1:0"})
			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), "missing or blank") {
				t.Fatalf("serve error = %v, want missing-or-blank error", err)
			}
		})
	}
}

func TestServeAuthenticationSupportsGoogleWithoutSharedSecret(t *testing.T) {
	const environmentName = "CROSSWALK_TEST_GOOGLE_ONLY_SECRET"
	t.Setenv(environmentName, "")
	clearGoogleAuthEnvironment(t)
	options := serveOptions{
		sharedSecretEnv:     environmentName,
		googleAudience:      "script-client.apps.googleusercontent.com",
		googleHostedDomain:  "example.edu",
		googleAllowedEmails: "person@example.edu, second@example.edu",
	}

	var received googleauth.Config
	secret, verifier, err := options.authentication(context.Background(), func(_ context.Context, cfg googleauth.Config) (httpapi.BearerVerifier, error) {
		received = cfg
		return commandVerifier{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if secret != "" || verifier == nil {
		t.Fatalf("authentication = secret %q verifier %T, want Google only", secret, verifier)
	}
	if received.Audience != options.googleAudience || received.HostedDomain != options.googleHostedDomain {
		t.Fatalf("Google config = %#v", received)
	}
	if got := strings.Join(received.AllowedEmails, ","); got != "person@example.edu,second@example.edu" {
		t.Fatalf("allowed emails = %q", got)
	}
}

func TestServeAuthenticationUsesEnvironmentAndPreservesSharedSecret(t *testing.T) {
	const environmentName = "CROSSWALK_TEST_COMBINED_SECRET"
	t.Setenv(environmentName, "test-secret")
	t.Setenv(googleAudienceEnv, "environment-client")
	t.Setenv(googleHostedDomainEnv, "example.edu")
	t.Setenv(googleAllowedEmailsEnv, "")
	options := serveOptions{sharedSecretEnv: environmentName}

	var received googleauth.Config
	secret, verifier, err := options.authentication(context.Background(), func(_ context.Context, cfg googleauth.Config) (httpapi.BearerVerifier, error) {
		received = cfg
		return commandVerifier{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if secret != "test-secret" || verifier == nil {
		t.Fatalf("authentication = secret %q verifier %T", secret, verifier)
	}
	if received.Audience != "environment-client" || received.HostedDomain != "example.edu" {
		t.Fatalf("Google config = %#v", received)
	}
}

func TestServeAuthenticationFailsClosedOnInvalidGoogleConfiguration(t *testing.T) {
	const environmentName = "CROSSWALK_TEST_INVALID_GOOGLE_SECRET"
	t.Setenv(environmentName, "")
	clearGoogleAuthEnvironment(t)
	options := serveOptions{
		sharedSecretEnv: environmentName,
		googleAudience:  "audience-without-an-identity-policy",
	}

	want := errors.New("invalid policy")
	_, _, err := options.authentication(context.Background(), func(_ context.Context, _ googleauth.Config) (httpapi.BearerVerifier, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("authentication() error = %v, want %v", err, want)
	}
}

func TestServeAuthenticationRejectsMissingConfiguredGoogleVerifier(t *testing.T) {
	const environmentName = "CROSSWALK_TEST_NIL_GOOGLE_VERIFIER_SECRET"
	t.Setenv(environmentName, "test-secret")
	clearGoogleAuthEnvironment(t)
	options := serveOptions{
		sharedSecretEnv:    environmentName,
		googleAudience:     "script-client.apps.googleusercontent.com",
		googleHostedDomain: "example.edu",
	}

	_, _, err := options.authentication(context.Background(), func(context.Context, googleauth.Config) (httpapi.BearerVerifier, error) {
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "verifier is unavailable") {
		t.Fatalf("authentication() error = %v, want unavailable verifier", err)
	}
}

func TestServeOptionsRejectDisabledSafetyLimits(t *testing.T) {
	valid := serveOptions{
		address:           "127.0.0.1:8080",
		sharedSecretEnv:   "SHARED_SECRET",
		maxBodyBytes:      1,
		maxOutputBytes:    1,
		maxRows:           1,
		maxCells:          1,
		maxConcurrent:     1,
		requestTimeout:    time.Second,
		readHeaderTimeout: time.Second,
		readTimeout:       time.Second,
		writeTimeout:      time.Second,
		idleTimeout:       time.Second,
		shutdownTimeout:   time.Second,
		maxHeaderBytes:    1,
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid options: %v", err)
	}

	tests := map[string]func(*serveOptions){
		"body limit":         func(options *serveOptions) { options.maxBodyBytes = 0 },
		"output limit":       func(options *serveOptions) { options.maxOutputBytes = 0 },
		"row limit":          func(options *serveOptions) { options.maxRows = 0 },
		"cell limit":         func(options *serveOptions) { options.maxCells = 0 },
		"concurrency limit":  func(options *serveOptions) { options.maxConcurrent = 0 },
		"request timeout":    func(options *serveOptions) { options.requestTimeout = 0 },
		"header limit":       func(options *serveOptions) { options.maxHeaderBytes = 0 },
		"header timeout":     func(options *serveOptions) { options.readHeaderTimeout = 0 },
		"read timeout":       func(options *serveOptions) { options.readTimeout = 0 },
		"write timeout":      func(options *serveOptions) { options.writeTimeout = 0 },
		"idle timeout":       func(options *serveOptions) { options.idleTimeout = 0 },
		"shutdown timeout":   func(options *serveOptions) { options.shutdownTimeout = 0 },
		"secret environment": func(options *serveOptions) { options.sharedSecretEnv = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			options := valid
			mutate(&options)
			if err := options.validate(); err == nil {
				t.Fatal("validate() error = nil")
			}
		})
	}
}

func TestServeDefaultsToLoopbackAndRequiresExplicitPublicHTTP(t *testing.T) {
	command := newServeCmd()
	if got := command.Flags().Lookup("address").DefValue; got != "127.0.0.1:8080" {
		t.Fatalf("default address = %q", got)
	}
	valid := serveOptions{
		address: "0.0.0.0:8080", sharedSecretEnv: "SHARED_SECRET",
		maxBodyBytes: 1, maxOutputBytes: 1, maxRows: 1, maxCells: 1,
		maxConcurrent: 1, maxHeaderBytes: 1,
		requestTimeout: time.Second, readHeaderTimeout: time.Second,
		readTimeout: time.Second, writeTimeout: time.Second,
		idleTimeout: time.Second, shutdownTimeout: time.Second,
	}
	if err := valid.validate(); err == nil || !strings.Contains(err.Error(), "refusing to expose") {
		t.Fatalf("public plaintext validate() error = %v", err)
	}
	valid.allowPublicHTTP = true
	if err := valid.validate(); err != nil {
		t.Fatalf("explicit public HTTP validate() error = %v", err)
	}
}
