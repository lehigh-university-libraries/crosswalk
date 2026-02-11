package proquestv1

import (
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	proquestv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/proquest/v1"
)

func TestComputeEmbargoDate(t *testing.T) {
	tests := []struct {
		name         string
		submission   *proquestv1.Submission
		wantDate     string
		wantDateType hubv1.DateType
	}{
		{
			name: "no embargo (code 0)",
			submission: &proquestv1.Submission{
				EmbargoCode: 0,
				Description: &proquestv1.Description{
					Dates: &proquestv1.Dates{
						AcceptDate: "01/15/2024",
					},
				},
			},
			wantDate: "",
		},
		{
			name: "6 month embargo (code 1)",
			submission: &proquestv1.Submission{
				EmbargoCode: 1,
				Description: &proquestv1.Description{
					Dates: &proquestv1.Dates{
						AcceptDate: "01/15/2024",
					},
				},
			},
			wantDate:     "2024-07-13", // 6 * 30 days = 180 days after 01/15/2024
			wantDateType: hubv1.DateType_DATE_TYPE_AVAILABLE,
		},
		{
			name: "12 month embargo (code 2)",
			submission: &proquestv1.Submission{
				EmbargoCode: 2,
				Description: &proquestv1.Description{
					Dates: &proquestv1.Dates{
						AcceptDate: "01/15/2024",
					},
				},
			},
			wantDate:     "2025-01-09", // 12 * 30 days = 360 days after 01/15/2024
			wantDateType: hubv1.DateType_DATE_TYPE_AVAILABLE,
		},
		{
			name: "24 month embargo (code 3)",
			submission: &proquestv1.Submission{
				EmbargoCode: 3,
				Description: &proquestv1.Description{
					Dates: &proquestv1.Dates{
						AcceptDate: "01/15/2024",
					},
				},
			},
			wantDate:     "2026-01-04", // 24 * 30 days = 720 days after 01/15/2024
			wantDateType: hubv1.DateType_DATE_TYPE_AVAILABLE,
		},
		{
			name: "explicit repository embargo overrides code",
			submission: &proquestv1.Submission{
				EmbargoCode: 1, // Would compute to ~6 months
				Repository: &proquestv1.Repository{
					Embargo: "2025-12-31",
				},
				Description: &proquestv1.Description{
					Dates: &proquestv1.Dates{
						AcceptDate: "01/15/2024",
					},
				},
			},
			wantDate:     "2025-12-31", // Explicit date takes precedence
			wantDateType: hubv1.DateType_DATE_TYPE_AVAILABLE,
		},
		{
			name: "repository embargo with MM/DD/YYYY format",
			submission: &proquestv1.Submission{
				Repository: &proquestv1.Repository{
					Embargo: "12/31/2025",
				},
			},
			wantDate:     "2025-12-31",
			wantDateType: hubv1.DateType_DATE_TYPE_AVAILABLE,
		},
		{
			name: "missing accept date with embargo code",
			submission: &proquestv1.Submission{
				EmbargoCode: 1,
				Description: &proquestv1.Description{
					Dates: &proquestv1.Dates{
						AcceptDate: "",
					},
				},
			},
			wantDate: "", // Can't compute without accept date
		},
		{
			name: "nil description",
			submission: &proquestv1.Submission{
				EmbargoCode: 1,
			},
			wantDate: "", // Can't compute without description
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := &hubv1.Record{
				Dates: make([]*hubv1.DateValue, 0),
			}

			err := ComputeEmbargoDate(tt.submission, record)
			if err != nil {
				t.Fatalf("ComputeEmbargoDate() error = %v", err)
			}

			if tt.wantDate == "" {
				// Expect no embargo date added
				for _, d := range record.Dates {
					if d.Type == hubv1.DateType_DATE_TYPE_AVAILABLE {
						t.Errorf("expected no embargo date, got %s", d.Raw)
					}
				}
				return
			}

			// Find the AVAILABLE date
			var foundDate *hubv1.DateValue
			for _, d := range record.Dates {
				if d.Type == tt.wantDateType {
					foundDate = d
					break
				}
			}

			if foundDate == nil {
				t.Fatalf("expected DATE_TYPE_AVAILABLE, got none")
			}

			if foundDate.Raw != tt.wantDate {
				t.Errorf("embargo date = %q, want %q", foundDate.Raw, tt.wantDate)
			}
		})
	}
}

func TestComputeEmbargoFromCode(t *testing.T) {
	tests := []struct {
		name       string
		code       int
		acceptDate string
		want       string
	}{
		{"code 0 - no embargo", 0, "01/15/2024", ""},
		{"code 1 - 6 months", 1, "01/15/2024", "2024-07-13"},
		{"code 2 - 12 months", 2, "01/15/2024", "2025-01-09"},
		{"code 3 - 24 months", 3, "01/15/2024", "2026-01-04"},
		{"unknown code", 4, "01/15/2024", ""},
		{"empty accept date", 1, "", ""},
		{"invalid accept date", 1, "invalid", ""},
		{"ISO format accept date", 1, "2024-01-15", "2024-07-13"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeEmbargoFromCode(tt.code, tt.acceptDate)
			if got != tt.want {
				t.Errorf("computeEmbargoFromCode(%d, %q) = %q, want %q", tt.code, tt.acceptDate, got, tt.want)
			}
		})
	}
}

func TestExtractEmbargoDate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"ISO format", "2025-12-31", "2025-12-31"},
		{"US format", "12/31/2025", "2025-12-31"},
		{"long format", "December 31, 2025", "2025-12-31"},
		{"short format", "Dec 31, 2025", "2025-12-31"},
		{"slash ISO", "2025/12/31", "2025-12-31"},
		{"empty", "", ""},
		{"unrecognized format", "31-Dec-2025", "31-Dec-2025"}, // Returns original if not parsed
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractEmbargoDate(tt.input)
			if got != tt.want {
				t.Errorf("extractEmbargoDate(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
