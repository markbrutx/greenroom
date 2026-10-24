#!/bin/sh
set -eu

cd "$(git rev-parse --show-toplevel)"

found=0
report="$(mktemp)"
trap 'rm -f "$report"' EXIT

scan() {
	awk -v kind="$1" '
		kind == "go" && /^\/\/go:(embed|build)/ { next }
		kind == "sh" && FNR == 1 && /^#!/ { next }
		kind == "go" && (/^[[:space:]]*\/\// || /\/\*/) { print FILENAME ":" FNR ": " $0; next }
		kind == "js" && (/^[[:space:]]*\/\// || /\/\*/) { print FILENAME ":" FNR ": " $0; next }
		kind == "css" && /\/\*/ { print FILENAME ":" FNR ": " $0; next }
		kind == "html" && /<!--/ { print FILENAME ":" FNR ": " $0; next }
		kind == "sql" && /^[[:space:]]*--/ { print FILENAME ":" FNR ": " $0; next }
		(kind == "hash" || kind == "sh") && /^[[:space:]]*#/ { print FILENAME ":" FNR ": " $0; next }
	' "$2" >>"$report"
}

git ls-files --cached --others --exclude-standard | while IFS= read -r file; do
	[ -f "$file" ] || continue
	case "$file" in
		LICENSE | *.md | go.sum | docs/img/*) continue ;;
	esac
	case "$file" in
		*.go) scan go "$file" ;;
		*.js) scan js "$file" ;;
		*.css) scan css "$file" ;;
		*.html) scan html "$file" ;;
		*.sql) scan sql "$file" ;;
		*.sh) scan sh "$file" ;;
		Makefile | */Makefile | Dockerfile | */Dockerfile | *.toml | *.yml | *.yaml | *.service | *.example | *.conf | *.env | .dockerignore | .gitignore | */.gitignore | Caddyfile | */Caddyfile) scan hash "$file" ;;
	esac
done

if [ -s "$report" ]; then
	cat "$report"
	found=1
fi

exit "$found"
