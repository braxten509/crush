#!/bin/bash
# Shows how Crush's copies in third_party/ differ from the upstream module
# versions go.mod pins, so the patches listed in each PATCHES.md can be
# checked. Usage: scripts/third-party-diff.sh [ultraviolet|vt|go-ansi-paintbrush] [--stat]
set -euo pipefail
cd "$(dirname "$0")/.."

declare -A modules=(
	[ultraviolet]=github.com/charmbracelet/ultraviolet
	[vt]=github.com/charmbracelet/x/vt
	[go-ansi-paintbrush]=github.com/jordanella/go-ansi-paintbrush
)

names=("${!modules[@]}")
stat=""
for arg in "$@"; do
	case "$arg" in
	--stat) stat=1 ;;
	*) names=("$arg") ;;
	esac
done

for name in "${names[@]}"; do
	module=${modules[$name]:?unknown copy: $name}
	version=$(go list -m -f '{{.Version}}' "$module")
	upstream=$(go mod download -json "$module@$version" | sed -n 's/.*"Dir": "\(.*\)".*/\1/p')
	echo "== third_party/$name vs $module@$version"
	if [ -n "$stat" ]; then
		diff -ruN -x '*_test.go' -x '*.md' -x 'go.sum' "$upstream" "third_party/$name" | grep '^+++ ' | sed 's#^+++ third_party/##; s#\t.*##' || true
	else
		diff -ruN -x '*_test.go' -x '*.md' -x 'go.sum' "$upstream" "third_party/$name" || true
	fi
done
