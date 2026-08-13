package spec

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// SupplementalMediaUseTIDDefault is the transformation default for the
	// Islandora Media Use term assigned to published supplemental files.
	SupplementalMediaUseTIDDefault = "supplemental.media_use_tid"
	// SupplementalPublishedDefault is the transformation default for the
	// Workbench published value assigned to published supplemental files.
	SupplementalPublishedDefault = "supplemental.published"
	// UnpublishedSupplementalMediaUseTIDDefault is the transformation default
	// for the Islandora Media Use term assigned to unpublished supplemental files.
	UnpublishedSupplementalMediaUseTIDDefault = "unpublished_supplemental.media_use_tid"
	// UnpublishedSupplementalPublishedDefault is the transformation default for
	// the Workbench published value assigned to unpublished supplemental files.
	UnpublishedSupplementalPublishedDefault = "unpublished_supplemental.published"
)

func validateWorkflowDefaults(defaults map[string]string) error {
	known := map[string]struct{}{
		FileStagingRootDefault: {}, FileAllowedAbsoluteRootsDefault: {},
		SupplementalMediaUseTIDDefault: {}, SupplementalPublishedDefault: {},
		UnpublishedSupplementalMediaUseTIDDefault: {}, UnpublishedSupplementalPublishedDefault: {},
	}
	for name := range defaults {
		if _, ok := known[name]; !ok {
			return fmt.Errorf("unsupported transformation default %q", name)
		}
	}
	if err := validateFileDefaults(defaults); err != nil {
		return err
	}
	for _, policy := range []struct {
		name, mediaUseKey, publishedKey string
	}{
		{name: "supplemental", mediaUseKey: SupplementalMediaUseTIDDefault, publishedKey: SupplementalPublishedDefault},
		{name: "unpublished supplemental", mediaUseKey: UnpublishedSupplementalMediaUseTIDDefault, publishedKey: UnpublishedSupplementalPublishedDefault},
	} {
		mediaUseTID, hasMediaUseTID := defaults[policy.mediaUseKey]
		published, hasPublished := defaults[policy.publishedKey]
		if hasMediaUseTID != hasPublished {
			return fmt.Errorf("%s workflow defaults %q and %q must be supplied together", policy.name, policy.mediaUseKey, policy.publishedKey)
		}
		if !hasMediaUseTID {
			continue
		}
		if mediaUseTID != strings.TrimSpace(mediaUseTID) || mediaUseTID == "" {
			return fmt.Errorf("invalid %s default: must be a positive decimal taxonomy term ID", policy.mediaUseKey)
		}
		value, err := strconv.ParseUint(mediaUseTID, 10, 64)
		if err != nil || value == 0 {
			return fmt.Errorf("invalid %s default %q: must be a positive decimal taxonomy term ID", policy.mediaUseKey, mediaUseTID)
		}
		if published != "0" && published != "1" {
			return fmt.Errorf("invalid %s default %q: must be 0 or 1", policy.publishedKey, published)
		}
	}
	return nil
}
