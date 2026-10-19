# mdpeek

`mdpeek <file.md>` は Markdown を静的 HTML に変換し、Google Chrome の新しいタブで開く macOS 向け CLI です。実行のたびに別の HTML を保存するため、前のタブと表示内容を残したまま見比べられます。HTTP サーバーや常駐プロセスは使いません。

Markdown の変換には [goldmark](https://github.com/yuin/goldmark) の GitHub Flavored Markdown 拡張を使います。日本語、表、タスクリスト、コードブロック、リンク、画像に対応します。

## インストール

Go 1.23 以上と、`/Applications/Google Chrome.app` にインストールされた Chrome が必要です。別の macOS でも、リポジトリを clone せずに導入できます。

```sh
go install github.com/YoshitsuguFujii/mdpeek@latest
mdpeek ./notes/design.md
```

`mdpeek: command not found` と表示されたら、Go のインストール先を `PATH` に追加してください。`GOBIN` を設定していない場合、通常は次の場所です。

```sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

`GOBIN` を設定している場合は、そのディレクトリを `PATH` に追加します。`go env GOBIN` で確認できます。

新しい公開版へ更新するときも、同じ `go install github.com/YoshitsuguFujii/mdpeek@latest` を実行します。タグより後の `main` を明示的に使いたい場合は `@main` を指定できます。バージョンを固定したい場合は、公開済みのタグまたはコミットを `@<version>` に指定してください。

ソースからインストール・ビルドする場合は、リポジトリのルートで実行します。

```sh
go install .
go build -o mdpeek .
```

## 使い方

```sh
mdpeek README.md
mdpeek ./docs/仕様.md
mdpeek /absolute/path/to/notes.markdown
```

引数には UTF-8 の `.md` または `.markdown` ファイルを1つ指定します。コマンドは生成した HTML のパスを表示して終了します。Markdownを編集して再実行すると、**毎回新しいタブ**と HTML が作られます。前のタブを再読み込みしても、当時の Markdown 本文が残ります。

Chromeが未起動、またはウィンドウがない場合は、背景でChromeを開きます。起動済みなら新しいタブを追加します。初回は macOS が Chrome の操作許可を求める場合があります。

プレビューには元ファイル名とパスを表示します。相対リンクと画像は Markdown ファイルの場所を基準に解決し、ページ内リンクはプレビュー内で移動します。長いページを下へスクロールすると、右下に先頭へ戻る「↑」が現れます。

## 保存先と表示履歴

生成した HTML は `~/.cache/mdpeek/preview-*.html` に残ります。保存先は `MDPEEK_OUTPUT_DIR` で変更できます。

```sh
MDPEEK_OUTPUT_DIR=/tmp/mdpeek-previews mdpeek ./notes/design.md
```

過去の HTML は自動削除しません。不要なファイルは手動で整理してください。削除した HTML を開いているタブは、その後再読み込みすると表示できなくなります。画像は元ファイルを参照するため、元画像を変更・削除すると過去のタブでも表示が変わります。HTMLにはMarkdown本文が含まれるので、保存先の共有やバックアップにも注意してください。

CSSと、先頭へ戻るボタンに必要な短いスクリプトはHTML内に含めています。変換時に外部CDNを呼ばず、プレビュー用サーバーも起動しません。

## Claude Code のフック（任意）

[フックのスクリプト](hooks/open-markdown-preview.sh)を設定すると、Claude Code の `Write`・`Edit` で対象の Markdown を更新したときに `mdpeek` を起動できます。フックには `jq` が必要で、Claude Code から見える `PATH` に `mdpeek` と `jq` を置いてください。

フックは作業リポジトリと `/tmp/claude-<uid>/<project>/<session>/scratchpad/` 以下の Markdown を対象にします。`CLAUDE.md`、`SKILL.md`、`AGENTS.md`、`~/.claude`、chezmoi の source は除外します。

スクリプトを配置する例です。この操作はリポジトリを clone して、そのルートで実行します。既存の同名ファイルがある場合は、上書き前に内容を確認してください。

```sh
mkdir -p "$HOME/.claude/scripts"
install -m 755 hooks/open-markdown-preview.sh "$HOME/.claude/scripts/open-markdown-preview.sh"
```

`~/.claude/settings.json` の `hooks.PostToolUse` 配列へ次の要素を追加します。他のフック設定は残してください。

```json
{
  "matcher": "Write|Edit",
  "hooks": [
    { "type": "command", "command": "~/.claude/scripts/open-markdown-preview.sh" }
  ]
}
```

chezmoiでフックを管理している場合は、配置先を直接編集せず、chezmoiのsourceへ同じスクリプトを反映して `chezmoi diff` と `chezmoi apply` で同期します。既存のフックを置換する場合も、設定を確認してから切り替えてください。

## 安全性と制限

- HTML は一時ファイルへ書き終えてから公開し、同時実行はロックで順序を決めます。入力エラー時に途中までのHTMLを公開しません。
- Markdown中の生HTMLと危険なURLは goldmark の既定設定で無効です。ファイル名と元パスは HTML エスケープし、プレビューには Content Security Policy を設定します。
- 入力ファイルと参照先のローカル画像・リンクは、そのPCに存在する必要があります。Markdown本文はスナップショットとして残りますが、画像そのものは複製しません。
- Chromeのインストール先を `/Applications/Google Chrome.app` として扱います。Linux・Windows のブラウザ起動には対応していません。
- Chromeを開けなかった場合も、エラーメッセージに保存済みHTMLのパスを示します。

## 開発時の確認

```sh
go test ./...
go vet ./...
go build -o mdpeek .
```
