package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func setupCLI(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	osa := "#!/bin/sh\nprintf '%s\\n' \"$2\" >> \"$MDPEEK_TEST_BROWSER_LOG\"\ncat >/dev/null\nif [ \"$MDPEEK_TEST_OSA_WARNING\" = 1 ]; then echo warning >&2; fi\nif [ \"$MDPEEK_TEST_OPEN\" = 1 ]; then printf 'OPEN\\n'; fi\n"
	opener := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$MDPEEK_TEST_OPEN_LOG\"\n"
	if err := os.WriteFile(filepath.Join(bin, "osascript"), []byte(osa), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "open"), []byte(opener), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MDPEEK_OUTPUT_DIR", filepath.Join(root, "output"))
	t.Setenv("MDPEEK_TEST_OPEN_LOG", filepath.Join(root, "open.log"))
	t.Setenv("MDPEEK_TEST_BROWSER_LOG", filepath.Join(root, "browser.log"))
	return root, filepath.Join(root, "output")
}

func markdown(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func generatePreview(t *testing.T, source string) string {
	t.Helper()
	var stdout bytes.Buffer
	if err := run([]string{source}, &stdout); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(stdout.String())
}

func preview(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestMarkdownPreviewIncludesContentAndSource(t *testing.T) {
	root, _ := setupCLI(t)
	path := filepath.Join(root, "日本語 folder", "設計.md")
	markdown(t, path, "# 日本語の見出し\n\n| 項目 | 値 |\n| --- | --- |\n| A | B |\n\n- [x] 完了\n\n~~~ruby\nputs 'hello'\n~~~\n\n![図](images/a.png)\n\n[次へ](next.md)\n")
	page := preview(t, generatePreview(t, path))
	for _, expected := range []string{"<title>設計.md</title>", "日本語の見出し", "<table>", "<td>B</td>", "checkbox", "checked", "puts", "images/a.png", "next.md", "%E6%97%A5%E6%9C%AC%E8%AA%9E%20folder/"} {
		if !strings.Contains(page, expected) {
			t.Errorf("preview missing %q", expected)
		}
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page, real) {
		t.Error("source path is not displayed")
	}
}

func TestTitleOptionSetsBrowserTabTitleAndKeepsSourceNameVisible(t *testing.T) {
	root, _ := setupCLI(t)
	path := filepath.Join(root, "notes.md")
	markdown(t, path, "# Notes")
	var stdout bytes.Buffer
	if err := run([]string{"--title", "設計 <A&B>", path}, &stdout); err != nil {
		t.Fatal(err)
	}
	page := preview(t, strings.TrimSpace(stdout.String()))
	if !strings.Contains(page, "<title>設計 &lt;A&amp;B&gt;</title>") {
		t.Fatal("custom title was not safely displayed in the browser tab")
	}
	if !strings.Contains(page, "<header><strong>notes.md</strong>") {
		t.Fatal("source filename was not displayed above the preview")
	}
}

func TestEmptyTitleOptionSetsAnEmptyBrowserTabTitle(t *testing.T) {
	root, _ := setupCLI(t)
	path := filepath.Join(root, "notes.md")
	markdown(t, path, "# Notes")
	var stdout bytes.Buffer
	if err := run([]string{"--title", "", path}, &stdout); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview(t, strings.TrimSpace(stdout.String())), "<title></title>") {
		t.Fatal("explicitly empty title was replaced with the filename")
	}
}

func TestUsageNamesMdpeekCommand(t *testing.T) {
	err := run(nil, &bytes.Buffer{})
	if err == nil || err.Error() != "usage: mdpeek [--title TITLE] <file.md>" {
		t.Fatalf("unexpected usage error: %v", err)
	}
}

func TestInvalidInputPreservesPreview(t *testing.T) {
	root, output := setupCLI(t)
	valid := filepath.Join(root, "valid.md")
	markdown(t, valid, "# Keep")
	snapshot := generatePreview(t, valid)
	original := preview(t, snapshot)
	for _, args := range [][]string{{}, {filepath.Join(root, "missing.md")}, {root}, {valid, valid}, {"--title"}, {"--unknown", valid}} {
		if err := run(args, &bytes.Buffer{}); err == nil {
			t.Errorf("expected input error for %v", args)
		}
		if preview(t, snapshot) != original {
			t.Errorf("input error replaced preview for %v", args)
		}
	}
	files, err := filepath.Glob(filepath.Join(output, "*.html"))
	if err != nil || len(files) != 1 {
		t.Fatalf("input errors created additional previews: %v: %v", files, err)
	}
}

func TestUnsafeMarkdownDoesNotCreateActiveContent(t *testing.T) {
	root, _ := setupCLI(t)
	path := filepath.Join(root, "unsafe.md")
	markdown(t, path, "# Safe\n\n<script>alert(1)</script>\n\n[x](javascript:alert(1))\n")
	page := strings.ToLower(preview(t, generatePreview(t, path)))
	if strings.Contains(page, "alert(1)") || strings.Contains(page, "href=\"javascript:") {
		t.Fatal("unsafe Markdown became active content")
	}
}

func TestReservedSourcePathCharactersAreEncoded(t *testing.T) {
	root, _ := setupCLI(t)
	path := filepath.Join(root, "draft# [1]", "note.md")
	markdown(t, path, "![図](photo.png)")
	if !strings.Contains(preview(t, generatePreview(t, path)), "draft%23%20%5B1%5D/") {
		t.Fatal("asset base was not URL-encoded")
	}
}

func TestRelativeOutputDirectoryOpensAnAbsoluteFileURL(t *testing.T) {
	root, _ := setupCLI(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "relative-output")
	relative, err := filepath.Rel(cwd, output)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MDPEEK_OUTPUT_DIR", relative)
	path := filepath.Join(root, "note.md")
	markdown(t, path, "# Note")
	snapshot := generatePreview(t, path)
	if !filepath.IsAbs(snapshot) {
		t.Fatalf("preview path is relative: %q", snapshot)
	}
	opened, err := os.ReadFile(filepath.Join(root, "browser.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(opened)) != "file://"+snapshot {
		t.Fatalf("Chrome received a non-absolute file URL: %q", opened)
	}
}

func TestPageLinkTargetsAHeadingInThePreview(t *testing.T) {
	root, _ := setupCLI(t)
	path := filepath.Join(root, "guide.md")
	markdown(t, path, "# Details\n\n[Jump](#details)\n")
	snapshot := generatePreview(t, path)
	page := preview(t, snapshot)
	link := regexp.MustCompile("<a href=\"([^\"]+)\">Jump</a>").FindStringSubmatch(page)
	if len(link) != 2 {
		t.Fatal("page link was not rendered")
	}
	base := &url.URL{Scheme: "file", Path: snapshot}
	if match := regexp.MustCompile("<base href=\"([^\"]+)\">").FindStringSubmatch(page); len(match) == 2 {
		parsed, err := url.Parse(match[1])
		if err != nil {
			t.Fatal(err)
		}
		base = parsed
	}
	reference, err := url.Parse(link[1])
	if err != nil {
		t.Fatal(err)
	}
	target := base.ResolveReference(reference)
	if target.Path != snapshot || target.Fragment != "details" {
		t.Fatalf("page link leaves the preview: %s", target)
	}
}

func TestLongPreviewOffersAWorkingLinkBackToTheTop(t *testing.T) {
	root, _ := setupCLI(t)
	path := filepath.Join(root, "long.md")
	markdown(t, path, "# Start\n\n"+strings.Repeat("A paragraph.\n\n", 100)+"## End\n")
	snapshot := generatePreview(t, path)
	page := preview(t, snapshot)
	if !strings.Contains(page, "id=\"mdpeek-top\"") {
		t.Fatal("preview has no top anchor")
	}
	if !strings.Contains(page, "href=\"#mdpeek-top\"") || !strings.Contains(page, "aria-label=\"先頭へ戻る\"") || !strings.Contains(page, ">↑</a>") {
		t.Fatal("preview has no back-to-top link")
	}
	if strings.Contains(page, "script-src 'unsafe-inline'") {
		t.Fatal("preview allows arbitrary inline scripts")
	}
	script := regexp.MustCompile("(?s)<script>(.*?)</script>").FindStringSubmatch(page)
	if len(script) != 2 {
		t.Fatal("preview has no visibility script")
	}
	digest := sha256.Sum256([]byte(script[1]))
	allowed := "script-src 'sha256-" + base64.StdEncoding.EncodeToString(digest[:]) + "'"
	if !strings.Contains(html.UnescapeString(page), allowed) {
		t.Fatal("content security policy blocks the visibility script")
	}
}

func TestConcurrentRunsPublishCompletePage(t *testing.T) {
	root, output := setupCLI(t)
	var paths []string
	for _, name := range []string{"a", "b"} {
		path := filepath.Join(root, name+".md")
		markdown(t, path, "# "+strings.Repeat(name, 20000))
		paths = append(paths, path)
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(paths))
	for _, path := range paths {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- run([]string{path}, &bytes.Buffer{}) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	files, err := filepath.Glob(filepath.Join(output, "*.html"))
	if err != nil || len(files) != 2 {
		t.Fatalf("concurrent runs did not keep two previews: %v: %v", files, err)
	}
	seen := map[string]bool{}
	for _, file := range files {
		page := preview(t, file)
		if strings.Count(page, "<html") != 1 || strings.Count(page, "</html>") != 1 {
			t.Fatal("preview is incomplete")
		}
		if strings.Contains(page, strings.Repeat("a", 20000)) {
			seen["a"] = true
		} else if strings.Contains(page, strings.Repeat("b", 20000)) {
			seen["b"] = true
		} else {
			t.Fatal("preview contains neither full source")
		}
	}
	if !seen["a"] || !seen["b"] {
		t.Fatal("one source was lost during concurrent runs")
	}
}

func TestChromeOpensInBackgroundWhenNoWindowExists(t *testing.T) {
	root, _ := setupCLI(t)
	t.Setenv("MDPEEK_TEST_OPEN", "1")
	path := filepath.Join(root, "note.md")
	markdown(t, path, "# Note")
	if err := run([]string{path}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "open.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "-g\n-a\n/Applications/Google Chrome.app\nfile:///") {
		t.Fatalf("Chrome was not opened in background: %q", data)
	}
}

func TestChromeOpensWhenAppleScriptWritesAWarning(t *testing.T) {
	root, _ := setupCLI(t)
	t.Setenv("MDPEEK_TEST_OPEN", "1")
	t.Setenv("MDPEEK_TEST_OSA_WARNING", "1")
	path := filepath.Join(root, "note.md")
	markdown(t, path, "# Note")
	if err := run([]string{path}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "open.log")); err != nil {
		t.Fatal("Chrome was not opened after a nonfatal AppleScript warning")
	}
}

func TestEachRunKeepsEarlierMarkdownInItsOwnPreview(t *testing.T) {
	root, output := setupCLI(t)
	first := filepath.Join(root, "first.md")
	second := filepath.Join(root, "second.md")
	markdown(t, first, "# First")
	markdown(t, second, "# Second")
	var snapshots []string
	for _, source := range []string{first, second, first} {
		if source == first && len(snapshots) == 2 {
			markdown(t, first, "# First updated")
		}
		var stdout bytes.Buffer
		if err := run([]string{source}, &stdout); err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, strings.TrimSpace(stdout.String()))
	}
	outputReal, err := filepath.EvalSymlinks(output)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"First", "Second", "First updated"} {
		if filepath.Dir(snapshots[i]) != outputReal {
			t.Fatalf("preview outside output directory: %q", snapshots[i])
		}
		data, err := os.ReadFile(snapshots[i])
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), want) {
			t.Errorf("snapshot %d lost its content", i)
		}
		if i == 0 && strings.Contains(string(data), "First updated") {
			t.Error("earlier snapshot changed after the source was edited")
		}
	}
	if snapshots[0] == snapshots[1] || snapshots[0] == snapshots[2] || snapshots[1] == snapshots[2] {
		t.Fatal("separate runs reused a preview file")
	}
	files, err := filepath.Glob(filepath.Join(output, "*.html"))
	if err != nil || len(files) != 3 {
		t.Fatalf("want three preview files, got %v: %v", files, err)
	}
	urls, err := os.ReadFile(filepath.Join(root, "browser.log"))
	if err != nil {
		t.Fatal(err)
	}
	opened := strings.Split(strings.TrimSpace(string(urls)), "\n")
	if len(opened) != len(snapshots) {
		t.Fatalf("Chrome received %d URLs for %d runs", len(opened), len(snapshots))
	}
	for i, got := range opened {
		if got != "file://"+snapshots[i] {
			t.Fatalf("Chrome received a different preview URL on run %d: %q", i, got)
		}
	}
}
