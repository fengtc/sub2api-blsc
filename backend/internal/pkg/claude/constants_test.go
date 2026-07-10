package claude

import "testing"

func TestDefaultModelsContainsClaudeFable5(t *testing.T) {
	for _, model := range DefaultModels {
		if model.ID == "claude-fable-5" {
			if model.DisplayName != "Claude Fable 5" {
				t.Fatalf("unexpected display name: %q", model.DisplayName)
			}
			return
		}
	}

	t.Fatal("expected claude-fable-5 in default Claude models")
}
