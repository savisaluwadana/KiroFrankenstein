// Package archtest enforces architecture rules, starting with the purity of
// internal/sim (no I/O, wall clock, global RNG or concurrency helpers).
package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const simPkgPath = "github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"

// simAllowedStdlib is the explicit allowlist of imports for non-test files
// in internal/sim. Anything else (time, math/rand, os, net, sync, ...) fails.
var simAllowedStdlib = map[string]bool{
	"bytes":           true,
	"cmp":             true,
	"container/heap":  true,
	"crypto/sha256":   true,
	"encoding/binary": true,
	"encoding/json":   true,
	"errors":          true,
	"fmt":             true,
	"hash/fnv":        true,
	"io":              true,
	"math":            true,
	"math/bits":       true,
	"slices":          true,
	"sort":            true,
	"strconv":         true,
	"strings":         true,
	"unicode/utf8":    true,
}

func importAllowed(path string) bool {
	if simAllowedStdlib[path] {
		return true
	}
	// The project's own internal/sim subpackages.
	return path == simPkgPath || strings.HasPrefix(path, simPkgPath+"/")
}

// forbiddenImports parses Go source and returns imports not on the allowlist.
func forbiddenImports(filename string, src any) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), filename, src, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var bad []string
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		if !importAllowed(p) {
			bad = append(bad, p)
		}
	}
	sort.Strings(bad)
	return bad, nil
}

func TestSimImportsAllowlist(t *testing.T) {
	simDir := filepath.Join("..", "sim")
	var checked int
	err := filepath.WalkDir(simDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		checked++
		bad, perr := forbiddenImports(path, nil)
		if perr != nil {
			t.Errorf("%s: parse: %v", path, perr)
			return nil
		}
		for _, b := range bad {
			t.Errorf("%s imports %q, which is not on the internal/sim allowlist", path, b)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", simDir, err)
	}
	if checked == 0 {
		t.Fatalf("no non-test Go files found under %s", simDir)
	}
	if _, err := os.Stat(simDir); err != nil {
		t.Fatal(err)
	}
}

func TestForbiddenImportDetected(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"clean", "package sim\nimport (\"fmt\"\n\"sort\")\n", nil},
		{"own subpackage", "package sim\nimport _ \"" + simPkgPath + "/money\"\n", nil},
		{"time", "package sim\nimport \"time\"\n", []string{"time"}},
		{"math/rand", "package sim\nimport \"math/rand\"\n", []string{"math/rand"}},
		{"os", "package sim\nimport \"os\"\n", []string{"os"}},
		{"net", "package sim\nimport \"net\"\n", []string{"net"}},
		{"sync", "package sim\nimport \"sync\"\n", []string{"sync"}},
		{"third party", "package sim\nimport \"pgregory.net/rapid\"\n", []string{"pgregory.net/rapid"}},
		{"mixed", "package sim\nimport (\"fmt\"\n\"time\"\n\"os\")\n", []string{"os", "time"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := forbiddenImports("x.go", tc.src)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
