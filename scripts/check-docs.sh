#!/usr/bin/env bash
# Check that relative links and images in the top-level Markdown files and
# docs/*.md resolve to existing files (and, for #anchors in the same file, that
# a matching heading exists). External URLs are not fetched.
# Usage: scripts/check-docs.sh   (run from anywhere; exits 1 on any failure)
set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT" || exit 2

fail=0
files=(README.md CONTRIBUTING.md SECURITY.md docs/*.md)
[ -d docs/hackathon ] && files+=(docs/hackathon/*.md)
[ -f docs/deploy-templates/README.md ] && files+=(docs/deploy-templates/README.md)

slug() { # GitHub-style heading anchor
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed -E 's/[`*]//g; s/[^a-z0-9 _-]//g; s/ /-/g'
}

for f in "${files[@]}"; do
  [ -f "$f" ] || continue
  dir="$(dirname "$f")"
  # Markdown links and images: ](target) and src="target"; code fences are skipped.
  targets="$(awk '/^```/{c=!c; next} !c' "$f" \
    | grep -oE '(\]\([^)]+\)|src="[^"]+")' \
    | sed -E 's/^\]\(//; s/\)$//; s/^src="//; s/"$//')"
  while IFS= read -r t; do
    [ -z "$t" ] && continue
    t="${t%% *}"                    # drop an optional title
    case "$t" in
      http://*|https://*|mailto:*) continue ;;
    esac
    path="${t%%#*}"; frag=""
    case "$t" in *#*) frag="${t#*#}" ;; esac
    if [ -z "$path" ]; then target="$f"; else
      case "$path" in /*) target=".$path" ;; *) target="$dir/$path" ;; esac
    fi
    if [ ! -e "$target" ]; then
      echo "BROKEN  $f -> $t"; fail=1; continue
    fi
    if [ -n "$frag" ] && [ -f "$target" ] && [[ "$target" == *.md ]]; then
      found=0
      while IFS= read -r h; do
        h="$(printf '%s' "$h" | sed -E 's/^#+ +//')"
        [ "$(slug "$h")" = "$frag" ] && { found=1; break; }
      done < <(awk '/^```/{c=!c; next} !c && /^#+ /' "$target")
      [ "$found" = 1 ] || { echo "NOANCHOR $f -> $t"; fail=1; }
    fi
  done <<< "$targets"
done

if [ "$fail" = 0 ]; then echo "docs ok: ${#files[@]} files checked"; fi
exit "$fail"
