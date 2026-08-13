package spec

import (
	"strings"
	"testing"
)

func TestValidateRejectsInvalidWorkflowDefaults(t *testing.T) {
	tests := []struct {
		name  string
		apply func(map[string]string)
		want  string
	}{
		{
			name: "unknown key",
			apply: func(defaults map[string]string) {
				defaults["supplemental.media_use_id"] = "16"
			},
			want: "unsupported transformation default",
		},
		{
			name: "half pair",
			apply: func(defaults map[string]string) {
				delete(defaults, SupplementalPublishedDefault)
			},
			want: "must be supplied together",
		},
		{
			name: "non numeric taxonomy term",
			apply: func(defaults map[string]string) {
				defaults[SupplementalMediaUseTIDDefault] = "http://pcdm.org/use#SupplementalFile"
			},
			want: "positive decimal taxonomy term ID",
		},
		{
			name: "zero taxonomy term",
			apply: func(defaults map[string]string) {
				defaults[UnpublishedSupplementalMediaUseTIDDefault] = "0"
			},
			want: "positive decimal taxonomy term ID",
		},
		{
			name: "invalid published value",
			apply: func(defaults map[string]string) {
				defaults[UnpublishedSupplementalPublishedDefault] = "yes"
			},
			want: "must be 0 or 1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transformation := FabricatorWorkbench()
			transformation.Fingerprint = Fingerprint{}
			test.apply(transformation.Defaults)
			if err := transformation.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestValidateAllowsOneCompleteSupplementalPolicy(t *testing.T) {
	transformation := FabricatorWorkbench()
	transformation.Fingerprint = Fingerprint{}
	delete(transformation.Defaults, UnpublishedSupplementalMediaUseTIDDefault)
	delete(transformation.Defaults, UnpublishedSupplementalPublishedDefault)
	if err := transformation.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
