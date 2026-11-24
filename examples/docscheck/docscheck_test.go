// Package docscheck checks the SDK's docs (the Markdown files of its folder, of docs/, of docs/tutorials/ and
// examples/README.md): every Go block appears word for word (leading whitespace aside) in a file of the examples,
// which the examples' tests compile and run; every relative link points at a file that exists; no placeholder is
// left for a value still to be filled in.
package docscheck

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func docs(t *testing.T) []string {
	t.Helper()
	root, err := filepath.Glob(filepath.Join("..", "..", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	tutorials, err := filepath.Glob(filepath.Join("..", "..", "docs", "tutorials", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(root) < 5 || len(files) < 6 || len(tutorials) < 5 {
		t.Fatalf("the docs are missing: %v %v %v", root, files, tutorials)
	}
	return append(append(append(root, filepath.Join("..", "README.md")), files...), tutorials...)
}

// normal is a text's non-empty lines without their leading and trailing whitespace.
func normal(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return "\n" + strings.Join(out, "\n") + "\n"
}

func examples(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir("..", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		b, err := os.ReadFile(path)
		files[path] = normal(string(b))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

var goBlock = regexp.MustCompile("(?s)```go\n(.*?)```")

func TestGoBlocksAreExamples(t *testing.T) {
	files := examples(t)
	blocks := 0
	for _, doc := range docs(t) {
		b, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range goBlock.FindAllStringSubmatch(string(b), -1) {
			blocks++
			block := normal(m[1])
			found := false
			for _, text := range files {
				found = found || strings.Contains(text, block)
			}
			if !found {
				t.Errorf("%s: this Go block is not in examples/ word for word:\n%s", doc, m[1])
			}
		}
	}
	if blocks < 8 {
		t.Fatalf("only %d Go blocks in the docs", blocks)
	}
}

var link = regexp.MustCompile(`\]\(([^)\s]+)\)`)

func TestRelativeLinks(t *testing.T) {
	for _, doc := range docs(t) {
		b, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range link.FindAllStringSubmatch(string(b), -1) {
			target := m[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			target, _, _ = strings.Cut(target, "#")
			if _, err := os.Stat(filepath.Join(filepath.Dir(doc), target)); err != nil {
				t.Errorf("%s: broken link %s", doc, m[1])
			}
		}
	}
}

// placeholder is a token a draft leaves for a value still to be filled in: an upper-case name with underscores
// (JUL30_END, RATE_SAMPLES), or TODO, TBD, FIXME or XXX. Code is not searched (fenced blocks and inline code spans),
// so the environment variables the docs name there (`PDIGGER_FACTORY_URL`) are not placeholders.
var placeholder = regexp.MustCompile(`\b(?:[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+|TODO|TBD|FIXME|XXX)\b`)

var inlineCode = regexp.MustCompile("`[^`]*`")

// placeholders lists the placeholders of a Markdown text outside its code, as "line n: TOKEN".
func placeholders(text string) []string {
	var found []string
	fenced := false
	for i, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		for _, token := range placeholder.FindAllString(inlineCode.ReplaceAllString(line, ""), -1) {
			found = append(found, fmt.Sprintf("line %d: %s", i+1, token))
		}
	}
	return found
}

// TestNoPlaceholders holds every doc to it, the shared copy docs/BEHAVIOUR-20.20.md included: a copy taken before its
// measurements were done fails here until the finished one is copied.
func TestNoPlaceholders(t *testing.T) {
	for _, doc := range docs(t) {
		b, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		if found := placeholders(string(b)); len(found) > 0 {
			t.Errorf("%s has placeholders: %s", doc, strings.Join(found, ", "))
		}
	}
}

func TestPlaceholderDetector(t *testing.T) {
	text := "a JUL30_END b\n`PDIGGER_FACTORY_URL` and TODO\n```sh\nX_Y=1 TBD\n```\n| RATE_SAMPLES | `C_D` | GET |\nFIXME"
	got := strings.Join(placeholders(text), ", ")
	if want := "line 1: JUL30_END, line 2: TODO, line 6: RATE_SAMPLES, line 7: FIXME"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
