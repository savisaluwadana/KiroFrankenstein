package llm

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoModelLiteral fails if a non-test source file contains a model
// identifier (no model is built in; LLM_MODEL is the only source).
func TestNoModelLiteral(t *testing.T) {
	re := regexp.MustCompile(`(?i)(gpt-|gpt4|claude|llama|gemini|mistral|mixtral|qwen|deepseek|\b(openai|anthropic|google|meta-llama|mistralai|deepseek)/)`)
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v (%d files)", err, len(files))
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if m := re.FindString(string(b)); m != "" {
			t.Errorf("%s contains model identifier literal %q", f, m)
		}
	}
}
