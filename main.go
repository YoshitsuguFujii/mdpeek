package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html"
	"html/template"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

const appleScript = `on run argv
  set previewURL to item 1 of argv
  if application "/Applications/Google Chrome.app" is not running then
    return "OPEN"
  end if
  tell application "/Applications/Google Chrome.app"
    if (count of windows) is 0 then return "OPEN"
    make new tab at end of tabs of front window with properties {URL:previewURL}
  end tell
end run
`

const stylesheet = `
:root { color-scheme: light dark; font-family: -apple-system, BlinkMacSystemFont, "Hiragino Kaku Gothic ProN", sans-serif; }
body { max-width: 860px; margin: 0 auto; padding: 32px 24px 96px; line-height: 1.75; overflow-wrap: anywhere; }
header { border-bottom: 1px solid #8886; padding-bottom: 18px; margin-bottom: 38px; }
header strong { display: block; font-size: 1.25rem; }
header small { display: block; color: #777; font-size: .78rem; overflow-wrap: anywhere; }
h1, h2, h3 { line-height: 1.35; margin-top: 1.7em; }
h1 { margin-top: 0; }
a { color: #2876bd; }
img { max-width: 100%; height: auto; }
pre { overflow-x: auto; padding: 16px; border-radius: 8px; background: #8882; line-height: 1.5; }
code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
:not(pre) > code { background: #8882; padding: .1em .3em; border-radius: 3px; }
table { border-collapse: collapse; display: block; overflow-x: auto; max-width: 100%; }
th, td { border: 1px solid #8888; padding: .45em .7em; }
th { background: #8882; }
blockquote { border-left: 3px solid #8888; padding-left: 1em; margin-left: 0; color: #777; }
ul:has(input[type=checkbox]) { list-style: none; padding-left: 1.5em; }
input[type=checkbox] { margin-left: -1.4em; margin-right: .45em; }
.back-to-top { position: fixed; right: 24px; bottom: 24px; z-index: 1; display: grid; place-items: center; width: 44px; height: 44px; border-radius: 50%; background: #2876bd; color: white; box-shadow: 0 3px 12px #0003; text-decoration: none; font-size: 1.3rem; }
.back-to-top:hover, .back-to-top:focus-visible { background: #185a97; color: white; }
.back-to-top[hidden] { display: none; }
@media (prefers-color-scheme: dark) { a { color: #79b8ef; } header small, blockquote { color: #aaa; } }
`

const backToTopScript = `const backToTop = document.querySelector(".back-to-top");
function updateBackToTop() {
  backToTop.hidden = document.documentElement.scrollHeight <= window.innerHeight || window.scrollY < 120;
}
window.addEventListener("scroll", updateBackToTop, { passive: true });
window.addEventListener("resize", updateBackToTop);
updateBackToTop();`

const pageSource = `<!doctype html>
<html lang="ja">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src file: data:; style-src 'unsafe-inline'; script-src 'sha256-{{.ScriptHash}}'">
<title>{{.Title}}</title>
<style>{{.CSS}}</style>
</head>
<body id="mdpeek-top">
<header><strong>{{.Title}}</strong><small>{{.Path}}</small></header>
<main>{{.Body}}</main>
<a class="back-to-top" href="#mdpeek-top" hidden aria-label="先頭へ戻る">↑</a>
<script>{{.Script}}</script>
</body>
</html>
`

var pageTemplate = template.Must(template.New("preview").Parse(pageSource))
var localURLAttribute = regexp.MustCompile("(<a href|<img src)=\"([^\"]*)\"")

func run(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: mdpeek <file.md>")
	}
	path, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".md" && ext != ".markdown" {
		return fmt.Errorf("Markdown file required (.md or .markdown)")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a file: %s", path)
	}
	source, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !utf8.Valid(source) {
		return fmt.Errorf("invalid UTF-8: %s", path)
	}

	outputDir := os.Getenv("MDPEEK_OUTPUT_DIR")
	if outputDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		outputDir = filepath.Join(home, ".cache", "mdpeek")
	}
	outputDir, err = filepath.Abs(outputDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return err
	}
	outputDir, err = filepath.EvalSymlinks(outputDir)
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(outputDir, "preview.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	page, err := render(path, source)
	if err != nil {
		return err
	}
	output, err := publish(outputDir, page)
	if err != nil {
		return err
	}
	if err := openBrowser(fileURL(output)); err != nil {
		return fmt.Errorf("preview saved at %s; %w", output, err)
	}
	_, err = fmt.Fprintln(stdout, output)
	return err
}

func render(path string, source []byte) ([]byte, error) {
	md := goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithParserOptions(parser.WithAutoHeadingID()))
	var body bytes.Buffer
	if err := md.Convert(source, &body); err != nil {
		return nil, err
	}
	base := &url.URL{Scheme: "file", Path: filepath.Dir(path) + string(os.PathSeparator)}
	content := localURLAttribute.ReplaceAllStringFunc(body.String(), func(attribute string) string {
		match := localURLAttribute.FindStringSubmatch(attribute)
		reference, err := url.Parse(html.UnescapeString(match[2]))
		if err != nil || reference.Scheme != "" || reference.Host != "" || reference.Path == "" {
			return attribute
		}
		return match[1] + "=\"" + html.EscapeString(base.ResolveReference(reference).String()) + "\""
	})
	data := struct {
		Title      string
		Path       string
		Body       template.HTML
		CSS        template.CSS
		Script     template.JS
		ScriptHash string
	}{
		Title:  filepath.Base(path),
		Path:   path,
		Body:   template.HTML(content), // goldmark omits raw HTML and unsafe links by default.
		CSS:    template.CSS(stylesheet),
		Script: template.JS(backToTopScript),
		ScriptHash: func() string {
			digest := sha256.Sum256([]byte(backToTopScript))
			return base64.StdEncoding.EncodeToString(digest[:])
		}(),
	}
	var page bytes.Buffer
	if err := pageTemplate.Execute(&page, data); err != nil {
		return nil, err
	}
	return page.Bytes(), nil
}

func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func publish(dir string, content []byte) (string, error) {
	temp, err := os.CreateTemp(dir, "preview-*.tmp")
	if err != nil {
		return "", err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return "", err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	output := strings.TrimSuffix(temp.Name(), ".tmp") + ".html"
	if err := os.Link(temp.Name(), output); err != nil {
		return "", err
	}
	return output, nil
}

func openBrowser(previewURL string) error {
	script := exec.Command("osascript", "-", previewURL)
	script.Stdin = strings.NewReader(appleScript)
	var stderr bytes.Buffer
	script.Stderr = &stderr
	result, err := script.Output()
	if err != nil {
		return fmt.Errorf("Chrome: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	if strings.TrimSpace(string(result)) != "OPEN" {
		return nil
	}
	opener := exec.Command("open", "-g", "-a", "/Applications/Google Chrome.app", previewURL)
	result, err = opener.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Chrome: %s: %w", strings.TrimSpace(string(result)), err)
	}
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "mdpeek:", err)
		os.Exit(1)
	}
}
