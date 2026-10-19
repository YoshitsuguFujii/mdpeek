package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupHook(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	bin := filepath.Join(root, "bin")
	scratch := filepath.Join(root, "claude-1000")
	for _, dir := range []string{project, bin, scratch} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	logPath := filepath.Join(root, "hook.log")
	fake := "#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$MDPEEK_TEST_HOOK_LOG\"\n"
	if err := os.WriteFile(filepath.Join(bin, "mdpeek"), []byte(fake), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", project)
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	t.Setenv("MARKDOWN_PREVIEW_SCRATCHPAD_ROOT", scratch)
	t.Setenv("MDPEEK_TEST_HOOK_LOG", logPath)
	return project, scratch, logPath
}

func invokeHook(t *testing.T, file, logPath string, expectCall bool) string {
	t.Helper()
	input, err := json.Marshal(map[string]any{
		"tool_name":  "Write",
		"tool_input": map[string]string{"file_path": file},
		"cwd":        os.Getenv("CLAUDE_PROJECT_DIR"),
	})
	if err != nil {
		t.Fatal(err)
	}
	hookPath, err := filepath.Abs(filepath.Join("hooks", "open-markdown-preview.sh"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", hookPath)
	if dir := os.Getenv("MDPEEK_TEST_HOOK_PROCESS_CWD"); dir != "" {
		command.Dir = dir
	}
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("hook failed: %v: %s", err, output)
	}
	wait := 300 * time.Millisecond
	if expectCall {
		wait = 2 * time.Second
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(logPath)
		if err == nil && len(data) > 0 {
			return strings.TrimSpace(string(data))
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ""
}

func TestHookPreviewsProjectAndScratchpadMarkdown(t *testing.T) {
	project, scratch, logPath := setupHook(t)
	projectFile := filepath.Join(project, "guide.md")
	markdown(t, projectFile, "# Guide")
	projectReal, err := filepath.EvalSymlinks(projectFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := invokeHook(t, projectFile, logPath, true); got != projectReal {
		t.Fatalf("project Markdown: got %q", got)
	}
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	scratchFile := filepath.Join(scratch, "proj", "session", "scratchpad", "drafts", "note.md")
	markdown(t, scratchFile, "# Draft")
	scratchReal, err := filepath.EvalSymlinks(scratchFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := invokeHook(t, scratchFile, logPath, true); got != scratchReal {
		t.Fatalf("scratchpad Markdown: got %q", got)
	}
}

func TestHookIgnoresConfigurationAndOutsidePaths(t *testing.T) {
	project, scratch, logPath := setupHook(t)
	paths := []string{
		filepath.Join(project, "CLAUDE.md"),
		filepath.Join(project, "SKILL.md"),
		filepath.Join(project, "AGENTS.md"),
		filepath.Join(project, ".claude", "plans", "note.md"),
		filepath.Join(project, ".local", "share", "chezmoi", "note.md"),
		filepath.Join(scratch, "proj", "session", "outside.md"),
		filepath.Join(filepath.Dir(project), "outside.md"),
	}
	for _, path := range paths {
		markdown(t, path, "# Ignore")
		if got := invokeHook(t, path, logPath, false); got != "" {
			t.Errorf("%s was previewed: %q", path, got)
		}
	}
}

func TestHookIgnoresSymlinkIntoExcludedDirectory(t *testing.T) {
	project, _, logPath := setupHook(t)
	target := filepath.Join(project, ".claude", "plans", "note.md")
	markdown(t, target, "# Ignore")
	link := filepath.Join(project, "linked.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if got := invokeHook(t, link, logPath, false); got != "" {
		t.Fatalf("excluded symlink was previewed: %q", got)
	}
}

func TestHookIgnoresNonMarkdownAndSymlinkOutsideProject(t *testing.T) {
	project, _, logPath := setupHook(t)
	textFile := filepath.Join(project, "note.txt")
	markdown(t, textFile, "Text")
	if got := invokeHook(t, textFile, logPath, false); got != "" {
		t.Fatalf("non-Markdown file was previewed: %q", got)
	}
	outside := filepath.Join(filepath.Dir(project), "outside.md")
	markdown(t, outside, "# Outside")
	link := filepath.Join(project, "linked.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if got := invokeHook(t, link, logPath, false); got != "" {
		t.Fatalf("symlink outside project was previewed: %q", got)
	}
}

func TestHookIgnoresSymlinkToConfigurationDocument(t *testing.T) {
	project, _, logPath := setupHook(t)
	workdir := filepath.Join(project, "workdir")
	if err := os.Mkdir(workdir, 0700); err != nil {
		t.Fatal(err)
	}
	markdown(t, filepath.Join(workdir, "l"), "glob candidate")
	t.Setenv("MDPEEK_TEST_HOOK_PROCESS_CWD", workdir)
	target := filepath.Join(project, "AGENTS.md")
	markdown(t, target, "# Instructions")
	link := filepath.Join(project, "linked.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if got := invokeHook(t, link, logPath, false); got != "" {
		t.Fatalf("configuration document was previewed through a symlink: %q", got)
	}
}
