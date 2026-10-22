#!/usr/bin/env bash
# The updater checks the fork's main branch by default (the owner, 2026-10-22:
# "we don't really need a desktop branch, a single main one works"). Builds a
# throwaway fork with a main and an older desktop branch and reads update.sh
# check's answer: its "latest" must be main's newest commit. Prints one
# PASS/FAIL line and exits with its own code.
set -u
cd "$(dirname "$0")/.."
TMP="$(mktemp -d /var/tmp/crush-update-branch.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT
bare="$TMP/fork.git"
git init -q --bare "$bare"
work="$TMP/work"
git init -q -b main "$work"
git -C "$work" config user.email test@example.com
git -C "$work" config user.name test
echo main >"$work/file"
git -C "$work" add file
git -C "$work" commit -qm "main's newest"
git -C "$work" push -q "$bare" main
main_tip="$(git -C "$work" rev-parse HEAD)"
git -C "$work" checkout -qb desktop
echo desktop >>"$work/file"
git -C "$work" commit -qam "an old desktop-only commit"
git -C "$work" push -q "$bare" desktop

out="$(CRUSH_UPDATE_REPO="$bare" bash update.sh check)"
if [ "$out" = "{\"current\":\"\",\"latest\":\"$main_tip\",\"newer\":true}" ]; then
  echo "PASS the updater checks the main branch by default"
  exit 0
fi
echo "FAIL the updater did not answer main's newest commit: $out"
exit 1