package strictjson

import (
	"strings"
	"testing"
)

func TestRejectDuplicateNamesAtEveryDepth(t *testing.T) {
	for _, input := range []string{
		`{"name":"first","name":"second"}`,
		`{"nested":{"name":"first","name":"second"}}`,
		`[{"name":"first","name":"second"}]`,
	} {
		if err := RejectDuplicateNames([]byte(input)); err == nil || !strings.Contains(err.Error(), `duplicate JSON member "name"`) {
			t.Errorf("RejectDuplicateNames(%s) error = %v", input, err)
		}
	}
	if err := RejectDuplicateNames([]byte(`{"left":{"name":"first"},"right":{"name":"second"}}`)); err != nil {
		t.Fatalf("distinct object members rejected: %v", err)
	}
}
