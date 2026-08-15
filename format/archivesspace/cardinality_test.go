package archivesspace

import (
	"encoding/json"
	"reflect"
	"testing"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

func TestPhysicalDescriptionsAndLanguagesPreserveEveryValue(t *testing.T) {
	record := &hubv1.Record{}
	model := &jsonModel{
		Notes: []note{
			{Type: "physdesc", Content: json.RawMessage(`"Physical note one"`)},
			{Type: "physdesc", Content: json.RawMessage(`"Physical note two"`)},
		},
		Extents: []extent{
			{Number: "12", ExtentType: "pages"},
			{Number: "1", ExtentType: "map"},
		},
	}
	firstLanguage := languageMaterial{}
	firstLanguage.LanguageAndScript.Language = "eng"
	secondLanguage := languageMaterial{}
	secondLanguage.LanguageAndScript.Language = "fre"
	model.LanguageMaterials = []languageMaterial{firstLanguage, secondLanguage}

	appendNotes(record, model, nil)
	appendExtents(record, model, nil)
	appendLanguages(record, model, nil)

	wantPhysicalDescriptions := []string{"Physical note one", "Physical note two", "12 pages", "1 map"}
	if got := hub.GetPhysicalDescriptions(record); !reflect.DeepEqual(got, wantPhysicalDescriptions) {
		t.Errorf("physical descriptions = %#v, want %#v", got, wantPhysicalDescriptions)
	}
	if got, want := hub.GetLanguages(record), []string{"eng", "fre"}; !reflect.DeepEqual(got, want) {
		t.Errorf("languages = %#v, want %#v", got, want)
	}
}
