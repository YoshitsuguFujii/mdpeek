#!/usr/bin/env bash
# Claude Code PostToolUse (Write|Edit): 選ばれたMarkdownだけを mdpeek に渡す。

command -v mdpeek >/dev/null 2>&1 || exit 0
command -v jq >/dev/null 2>&1 || exit 0

input=$(cat)
file_path=$(printf '%s' "$input" | jq -r '.tool_input.file_path // ""' 2>/dev/null) || exit 0
cwd=$(printf '%s' "$input" | jq -r '.cwd // ""' 2>/dev/null) || exit 0
[ -n "$file_path" ] || exit 0

case "$file_path" in
  /*) ;;
  *) file_path="$cwd/$file_path" ;;
esac
[ -f "$file_path" ] || exit 0

extension=$(printf '%s' "${file_path##*.}" | tr '[:upper:]' '[:lower:]')
case "$extension" in
  md|markdown) ;;
  *) exit 0 ;;
esac

case "$(basename "$file_path" | tr '[:lower:]' '[:upper:]')" in
  CLAUDE.MD|SKILL.MD|AGENTS.MD) exit 0 ;;
esac

real_file=$(realpath "$file_path" 2>/dev/null) || exit 0
case "$(basename "$real_file" | tr '[:lower:]' '[:upper:]')" in
  CLAUDE.MD|SKILL.MD|AGENTS.MD) exit 0 ;;
esac
project=$(realpath "${CLAUDE_PROJECT_DIR:-$cwd}" 2>/dev/null) || project=''

in_project=false
case "$real_file" in
  "$project"/*) [ -n "$project" ] && in_project=true ;;
esac

in_scratchpad=false
root=$(realpath "${MARKDOWN_PREVIEW_SCRATCHPAD_ROOT:-/tmp/claude-$(id -u)}" 2>/dev/null) || root=''
if [ -n "$root" ]; then
  case "$real_file" in
    "$root"/*)
      rest=${real_file#"$root"/}
      project_name=${rest%%/*}
      rest=${rest#*/}
      session=${rest%%/*}
      case "$real_file" in
        "$root/$project_name/$session/scratchpad"/*) in_scratchpad=true ;;
      esac
      ;;
  esac
fi

[ "$in_project" = true ] || [ "$in_scratchpad" = true ] || exit 0

for excluded in "$HOME/.claude" "${CHEZMOI_SOURCE_DIR:-$HOME/.local/share/chezmoi}"; do
  excluded_dir=$(realpath "$excluded" 2>/dev/null) || continue
  case "$real_file" in
    "$excluded_dir"/*) exit 0 ;;
  esac
done

mdpeek "$real_file" </dev/null >/dev/null 2>&1 &
exit 0
