package islandora_workbench

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

func TestProfileWorkbenchValueRejectsUnknownDrupalAttributes(t *testing.T) {
	tests := []struct {
		name       string
		sourceType string
		value      map[string]any
		wantErr    string
	}{
		{
			name:       "scalar",
			sourceType: "string",
			value:      map[string]any{"value": "kept", "unexpected": "lost"},
			wantErr:    "unexpected attributes",
		},
		{
			name:       "link",
			sourceType: "link",
			value:      map[string]any{"uri": "https://example.edu", "description": "lost"},
			wantErr:    `unsupported attribute "description"`,
		},
		{
			name:       "geolocation",
			sourceType: "geolocation",
			value:      map[string]any{"lat": "40", "lng": "-75", "altitude": "100"},
			wantErr:    `unsupported attribute "altitude"`,
		},
		{
			name:       "structured",
			sourceType: "related_item",
			value:      map[string]any{"title": "Journal", "subtitle": "lost"},
			wantErr:    `unsupported attribute "subtitle"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := profileWorkbenchValue(
				spec.Field{},
				profile.ResolvedField{SourceType: test.sourceType},
				test.value,
				"",
			)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("profileWorkbenchValue() error = %v, want containing %q", err, test.wantErr)
			}
			if got != "" {
				t.Fatalf("profileWorkbenchValue() = %q on rejected object, want empty", got)
			}
		})
	}
}

func TestRecordToColumnsEnforcesSupplementalArtifactContract(t *testing.T) {
	tests := []struct {
		name    string
		files   []*hubv1.File
		want    string
		wantErr string
	}{
		{
			name:  "one supplemental path stays on parent row",
			files: []*hubv1.File{{Path: "supplement.pdf", Role: "supplemental"}},
			want:  "supplement.pdf",
		},
		{
			name: "multiple supplemental paths require planning",
			files: []*hubv1.File{
				{Path: "supplement-1.pdf", Role: "supplemental"},
				{Path: "supplement-2.pdf", Role: "supplemental"},
			},
			wantErr: "artifact planning",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			columns, _, err := recordToColumns(&hubv1.Record{Files: test.files}, "|")
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("recordToColumns() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("recordToColumns() error = %v", err)
			}
			if got := columns["supplemental_file"]; got != test.want {
				t.Fatalf("supplemental_file = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPlanArtifactsSplitsSupplementalOverflowBeforeStrictSerialization(t *testing.T) {
	record := &hubv1.Record{Files: []*hubv1.File{
		{Path: "supplement-1.pdf", Role: "supplemental"},
		{Path: "supplement-2.pdf", Role: "supplemental"},
	}}
	hub.SetExtra(record, "node_id", "123")

	plan, err := PlanArtifacts([]*hubv1.Record{record}, &format.SerializeOptions{
		Spec:          spec.FabricatorWorkbench(),
		IncludeHeader: true,
	})
	if err != nil {
		t.Fatalf("PlanArtifacts() error = %v", err)
	}
	if len(plan.Artifacts) != 3 {
		t.Fatalf("artifacts = %v, want update, add-media, and manifest", artifactNames(plan.Artifacts))
	}
	if got := artifactNames(plan.Artifacts); strings.Join(got, "|") != "target.update.csv|target.add_media.csv|"+ArtifactManifestName {
		t.Fatalf("artifacts = %v, want update, add-media, and manifest", got)
	}
	assertCSVValue(t, readArtifactCSV(t, plan.Artifacts[0].Data), "supplemental_file", "/mnt/islandora_staging/supplement-1.pdf")
	assertCSVValue(t, readArtifactCSV(t, plan.Artifacts[1].Data), "file", "/mnt/islandora_staging/supplement-2.pdf")
}

func TestTargetFieldValueWarnsForUnmappedHubPath(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	got, err := targetFieldValue(&hubv1.Record{}, "Unmapped.Path", map[string]string{}, "|")
	if err != nil {
		t.Fatalf("targetFieldValue() error = %v", err)
	}
	if got != "" {
		t.Fatalf("targetFieldValue() = %q, want empty", got)
	}
	logOutput := logs.String()
	for _, want := range []string{"level=WARN", "Islandora Workbench target has no mapping for Hub path", "hub_path=Unmapped.Path"} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("warning = %q, want containing %q", logOutput, want)
		}
	}
}
