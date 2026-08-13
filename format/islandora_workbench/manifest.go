package islandora_workbench

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

const (
	// ArtifactManifestName is the stable Workbench bundle manifest filename.
	ArtifactManifestName = "crosswalk-artifacts.json"
	// ArtifactManifestMediaType is the media type of the Workbench bundle manifest.
	ArtifactManifestMediaType = "application/json"
	// ArtifactManifestVersion is the current Workbench bundle manifest schema.
	ArtifactManifestVersion = 1
	// ArtifactContractVersion is the separately provisioned trust-anchor schema
	// consumed by sitectl-isle for Crosswalk Workbench bundles.
	ArtifactContractVersion = 1
	// ArtifactManifestPathModeStagedPOSIX identifies Crosswalk's staging-root
	// normalization for relative and non-allowlisted absolute file paths.
	ArtifactManifestPathModeStagedPOSIX = "staged-posix"
)

// ArtifactManifest describes one deterministic Crosswalk Workbench bundle.
// Its Artifacts entries never include or digest the manifest itself.
type ArtifactManifest struct {
	Version            int                     `json:"version"`
	Spec               ArtifactManifestSpec    `json:"spec"`
	ProfileFingerprint string                  `json:"profile_fingerprint,omitempty"`
	ModelFingerprint   string                  `json:"model_fingerprint,omitempty"`
	Policy             *ArtifactManifestPolicy `json:"policy,omitempty"`
	Artifacts          []ArtifactManifestEntry `json:"artifacts"`
}

// ArtifactContract is the stable, artifact-free subset of a manifest that a
// site operator reviews and provisions independently from uploaded batches.
// It is an integrity trust anchor, so callers must not derive it from an
// unreviewed batch manifest.
type ArtifactContract struct {
	Version            int                     `json:"version"`
	Spec               ArtifactManifestSpec    `json:"spec"`
	ProfileFingerprint string                  `json:"profile_fingerprint,omitempty"`
	ModelFingerprint   string                  `json:"model_fingerprint,omitempty"`
	Policy             *ArtifactManifestPolicy `json:"policy,omitempty"`
}

// ArtifactManifestSpec identifies the exact transformation specification.
type ArtifactManifestSpec struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint"`
}

// ArtifactManifestPolicy records only Workbench policy explicitly known from
// the transformation specification. Nil AllowedAbsoluteRoots means path policy
// is unavailable; a non-nil empty slice means no absolute roots may pass through.
type ArtifactManifestPolicy struct {
	PathMode                           string    `json:"path_mode,omitempty"`
	StagingRoot                        string    `json:"staging_root,omitempty"`
	AllowedAbsoluteRoots               *[]string `json:"allowed_absolute_roots,omitempty"`
	SupplementalMediaUseTID            string    `json:"supplemental_media_use_tid,omitempty"`
	PendingSupplementalPublished       string    `json:"pending_supplemental_published,omitempty"`
	UnpublishedSupplementalMediaUseTID string    `json:"unpublished_supplemental_media_use_tid,omitempty"`
	UnpublishedSupplementalPublished   string    `json:"unpublished_supplemental_published,omitempty"`
}

// ArtifactManifestEntry records the integrity and row count of one CSV artifact.
type ArtifactManifestEntry struct {
	Path      string `json:"path"`
	MediaType string `json:"media_type"`
	SHA256    string `json:"sha256"`
	Bytes     int64  `json:"bytes"`
	CSVRows   int    `json:"csv_rows"`
}

// BuildArtifactContract builds a trusted site contract from a reviewed sealed
// specification and, when supplied, the exact published target profile. The
// same spec-to-model binding enforced for batch manifests is enforced here.
func BuildArtifactContract(transformation *spec.Transformation, systemProfile *profile.Compiled) (ArtifactContract, error) {
	if transformation == nil {
		return ArtifactContract{}, fmt.Errorf("building Workbench artifact contract requires a transformation specification")
	}
	if err := transformation.Validate(); err != nil {
		return ArtifactContract{}, fmt.Errorf("invalid transformation specification: %w", err)
	}
	if err := transformation.ValidateSealed(); err != nil {
		return ArtifactContract{}, fmt.Errorf("unsealed transformation specification: %w", err)
	}
	if err := validateArtifactProfileBinding(transformation, systemProfile); err != nil {
		return ArtifactContract{}, err
	}
	fingerprint, err := transformationFingerprint(transformation)
	if err != nil {
		return ArtifactContract{}, err
	}
	contract := ArtifactContract{
		Version: ArtifactContractVersion,
		Spec: ArtifactManifestSpec{
			Name: transformation.Name, Version: transformation.Version, Fingerprint: fingerprint,
		},
		Policy: artifactManifestPolicy(transformation),
	}
	if systemProfile != nil {
		contract.ProfileFingerprint = systemProfile.Fingerprint()
		contract.ModelFingerprint = systemProfile.ModelFingerprint()
		if (contract.ProfileFingerprint == "") != (contract.ModelFingerprint == "") {
			return ArtifactContract{}, fmt.Errorf("target profile and model fingerprints must be supplied together")
		}
		if transformation.Fingerprint.Model != contract.ModelFingerprint {
			return ArtifactContract{}, fmt.Errorf("transformation model fingerprint does not match target profile model")
		}
		if transformation.Fingerprint.Profile != contract.ProfileFingerprint {
			return ArtifactContract{}, fmt.Errorf("transformation profile fingerprint does not match target profile")
		}
		if err := validateLowerSHA256("target profile fingerprint", contract.ProfileFingerprint); err != nil {
			return ArtifactContract{}, err
		}
		if err := validateLowerSHA256("target model fingerprint", contract.ModelFingerprint); err != nil {
			return ArtifactContract{}, err
		}
	}
	return contract, nil
}

func appendArtifactManifest(plan *ArtifactPlan, opts *format.SerializeOptions) error {
	if plan == nil || opts == nil || opts.Spec == nil {
		return fmt.Errorf("planning Workbench manifest requires a transformation specification")
	}
	fingerprint, err := transformationFingerprint(opts.Spec)
	if err != nil {
		return err
	}
	if err := validateArtifactProfileBinding(opts.Spec, opts.SystemProfile); err != nil {
		return err
	}
	manifest := ArtifactManifest{
		Version: ArtifactManifestVersion,
		Spec: ArtifactManifestSpec{
			Name:        opts.Spec.Name,
			Version:     opts.Spec.Version,
			Fingerprint: fingerprint,
		},
		Policy:    artifactManifestPolicy(opts.Spec),
		Artifacts: make([]ArtifactManifestEntry, 0, len(plan.Artifacts)),
	}
	if opts.SystemProfile != nil {
		manifest.ProfileFingerprint = opts.SystemProfile.Fingerprint()
		manifest.ModelFingerprint = opts.SystemProfile.ModelFingerprint()
		if (manifest.ProfileFingerprint == "") != (manifest.ModelFingerprint == "") {
			return fmt.Errorf("target profile and model fingerprints must be supplied together")
		}
		if opts.Spec.Fingerprint.Model != manifest.ModelFingerprint {
			return fmt.Errorf("transformation model fingerprint does not match target profile model")
		}
		if opts.Spec.Fingerprint.Profile != manifest.ProfileFingerprint {
			return fmt.Errorf("transformation profile fingerprint does not match target profile")
		}
		if manifest.ProfileFingerprint != "" {
			if err := validateLowerSHA256("target profile fingerprint", manifest.ProfileFingerprint); err != nil {
				return err
			}
			if err := validateLowerSHA256("target model fingerprint", manifest.ModelFingerprint); err != nil {
				return err
			}
		}
	}

	seen := make(map[string]struct{}, len(plan.Artifacts)+1)
	seen[ArtifactManifestName] = struct{}{}
	for _, artifact := range plan.Artifacts {
		if err := validateManifestArtifact(artifact, seen); err != nil {
			return err
		}
		rows, err := artifactCSVRows(artifact)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(artifact.Data)
		manifest.Artifacts = append(manifest.Artifacts, ArtifactManifestEntry{
			Path:      artifact.Name,
			MediaType: artifact.MediaType,
			SHA256:    hex.EncodeToString(digest[:]),
			Bytes:     int64(len(artifact.Data)),
			CSVRows:   rows,
		})
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding Workbench artifact manifest: %w", err)
	}
	data = append(data, '\n')
	plan.Artifacts = append(plan.Artifacts, Artifact{
		Name:      ArtifactManifestName,
		MediaType: ArtifactManifestMediaType,
		Data:      data,
	})
	return nil
}

func validateArtifactProfileBinding(transformation *spec.Transformation, systemProfile *profile.Compiled) error {
	if transformation == nil {
		return fmt.Errorf("Workbench profile binding requires a transformation specification")
	}
	profileFingerprint := strings.TrimSpace(transformation.Fingerprint.Profile)
	if profileFingerprint != "" && systemProfile == nil {
		return fmt.Errorf("profile-bound transformation requires the exact target system profile")
	}
	if profileFingerprint == "" && systemProfile != nil {
		return fmt.Errorf("unbound transformation cannot use a target system profile")
	}
	if systemProfile != nil && systemProfile.System() != "drupal" {
		return fmt.Errorf("Islandora Workbench target profile system %q is not drupal", systemProfile.System())
	}
	if systemProfile != nil && transformation.Fingerprint.Profile != systemProfile.Fingerprint() {
		return fmt.Errorf("transformation profile fingerprint does not match target profile")
	}
	if systemProfile != nil && transformation.Fingerprint.Model != systemProfile.ModelFingerprint() {
		return fmt.Errorf("transformation model fingerprint does not match target profile model")
	}
	return nil
}

func transformationFingerprint(transformation *spec.Transformation) (string, error) {
	value := strings.TrimSpace(transformation.Fingerprint.Value)
	if value == "" {
		computed, err := transformation.ComputeFingerprint()
		if err != nil {
			return "", fmt.Errorf("computing Workbench transformation fingerprint: %w", err)
		}
		value = computed
	}
	value = strings.ToLower(value)
	if err := validateLowerSHA256("transformation fingerprint", value); err != nil {
		return "", err
	}
	return value, nil
}

func validateLowerSHA256(label, value string) error {
	if len(value) != sha256.Size*2 {
		return fmt.Errorf("%s must contain a SHA-256 digest", label)
	}
	if _, err := hex.DecodeString(value); err != nil || value != strings.ToLower(value) {
		return fmt.Errorf("%s must contain lowercase SHA-256 hexadecimal", label)
	}
	return nil
}

func artifactManifestPolicy(transformation *spec.Transformation) *ArtifactManifestPolicy {
	policy := &ArtifactManifestPolicy{
		SupplementalMediaUseTID:            transformation.Default(spec.SupplementalMediaUseTIDDefault),
		PendingSupplementalPublished:       transformation.Default(spec.SupplementalPublishedDefault),
		UnpublishedSupplementalMediaUseTID: transformation.Default(spec.UnpublishedSupplementalMediaUseTIDDefault),
		UnpublishedSupplementalPublished:   transformation.Default(spec.UnpublishedSupplementalPublishedDefault),
	}
	if stagingRoot := strings.TrimSpace(transformation.Default(spec.FileStagingRootDefault)); stagingRoot != "" {
		roots := make([]string, 0)
		if raw := transformation.Default(spec.FileAllowedAbsoluteRootsDefault); strings.TrimSpace(raw) != "" {
			for _, root := range strings.Split(raw, "|") {
				roots = append(roots, strings.TrimSpace(root))
			}
		}
		policy.PathMode = ArtifactManifestPathModeStagedPOSIX
		policy.StagingRoot = stagingRoot
		policy.AllowedAbsoluteRoots = &roots
	}
	if policy.PathMode == "" && policy.SupplementalMediaUseTID == "" && policy.PendingSupplementalPublished == "" &&
		policy.UnpublishedSupplementalMediaUseTID == "" && policy.UnpublishedSupplementalPublished == "" {
		return nil
	}
	return policy
}

func validateManifestArtifact(artifact Artifact, seen map[string]struct{}) error {
	if artifact.Name == "" || artifact.Name == "." || artifact.Name == ".." || path.Base(artifact.Name) != artifact.Name || strings.Contains(artifact.Name, `\`) {
		return fmt.Errorf("invalid Workbench artifact name %q", artifact.Name)
	}
	if _, exists := seen[artifact.Name]; exists {
		return fmt.Errorf("duplicate Workbench artifact name %q", artifact.Name)
	}
	seen[artifact.Name] = struct{}{}
	if strings.TrimSpace(artifact.MediaType) == "" {
		return fmt.Errorf("Workbench artifact %q has no media type", artifact.Name)
	}
	if artifact.Records < 0 {
		return fmt.Errorf("Workbench artifact %q has a negative CSV row count", artifact.Name)
	}
	return nil
}

func artifactCSVRows(artifact Artifact) (int, error) {
	mediaType := strings.ToLower(strings.TrimSpace(artifact.MediaType))
	if mediaType != "text/csv" && !strings.HasPrefix(mediaType, "text/csv;") {
		if artifact.Records != 0 {
			return 0, fmt.Errorf("non-CSV Workbench artifact %q declares %d CSV rows", artifact.Name, artifact.Records)
		}
		return 0, nil
	}
	rows, err := csv.NewReader(bytes.NewReader(artifact.Data)).ReadAll()
	if err != nil {
		return 0, fmt.Errorf("reading Workbench artifact %q for manifest: %w", artifact.Name, err)
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("Workbench CSV artifact %q has no header row", artifact.Name)
	}
	dataRows := len(rows) - 1
	if artifact.Records != dataRows {
		return 0, fmt.Errorf("Workbench artifact %q declares %d CSV rows but contains %d", artifact.Name, artifact.Records, dataRows)
	}
	return dataRows, nil
}
