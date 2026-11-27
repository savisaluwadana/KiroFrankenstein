package llm

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestEnvExample(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	lines := strings.Split(s, "\n")
	has := func(l string) bool {
		for _, x := range lines {
			if strings.TrimSpace(x) == l {
				return true
			}
		}
		return false
	}
	for _, l := range []string{"LLM_BASE_URL=https://openrouter.ai/api/v1", "LLM_API_KEY=", "LLM_MODEL="} {
		if !has(l) {
			t.Errorf(".env.example lacks line %q", l)
		}
	}
	for _, v := range []string{EnvTimeoutSeconds, EnvMaxCalls, EnvMaxTokens, EnvMaxTotalTokens, EnvHTTPReferer, EnvAppTitle, EnvMaxPromptBytes, EnvGlobalMaxCalls, EnvGlobalMaxTokens, EnvGlobalWindow, EnvMaxConcurrentCalls} {
		if !regexp.MustCompile(`(?m)^# ` + v + `=`).MatchString(s) {
			t.Errorf(".env.example lacks commented placeholder for %s", v)
		}
	}
}
