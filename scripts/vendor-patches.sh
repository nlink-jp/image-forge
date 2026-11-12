#!/bin/sh
# vendor-patches.sh — apply, verify or revert the local patches to the vendored
# sources (third_party/patches/series).
#
# Usage: vendor-patches.sh apply | verify | revert
#
#   apply   Apply each patch that is not applied yet, in series order. A patch
#           that neither is applied nor applies cleanly stops the build: the
#           pinned upstream moved under it and the patch needs a decision.
#   verify  Fail unless every patched directory is exactly "pinned upstream +
#           its patches": each patch is present, and the directory's diff
#           against its pinned commit touches the same files by the same line
#           counts as the patches do. A hand edit to the vendored tree, or a
#           patch that no longer describes it, fails here.
#   revert  Remove the patches, in reverse order — do this before moving the
#           stable-diffusion.cpp pin, then apply again and see what breaks.
#
# The vendored trees are git checkouts pinned by the submodule pointer, so the
# pinned upstream is already on disk and no network is needed.

set -eu

MODE="${1:?Usage: $0 apply|verify|revert}"
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
PATCH_DIR="$ROOT/third_party/patches"
SERIES="$PATCH_DIR/series"

die() { echo "[vendor-patches] $1" >&2; exit 1; }
[ -f "$SERIES" ] || die "no series file: $SERIES"

# "patch dir" pairs, comments and blank lines dropped.
entries() { sed -e 's/#.*//' -e '/^[[:space:]]*$/d' "$SERIES" | awk '{print $1 " " $2}'; }

# Sum of added/deleted lines per file, sorted: the shape of a set of changes.
shape() { awk '{a[$3]+=$1; d[$3]+=$2} END {for (f in a) print f, a[f], d[f]}' | sort; }

case "$MODE" in
  apply)
    entries | while read -r patch dir; do
      [ -f "$PATCH_DIR/$patch" ] || die "series names a missing patch: $patch"
      [ -d "$ROOT/$dir" ] || die "$patch: no such directory: $dir (git submodule update --init --recursive)"
      if git -C "$ROOT/$dir" apply --reverse --check "$PATCH_DIR/$patch" 2>/dev/null; then
        continue
      fi
      git -C "$ROOT/$dir" apply --check "$PATCH_DIR/$patch" ||
        die "$patch does not apply to $dir: the pinned upstream changed under it"
      git -C "$ROOT/$dir" apply "$PATCH_DIR/$patch"
      echo "[vendor-patches] applied $patch"
    done
    ;;
  revert)
    entries | sed '1!G;h;$!d' | while read -r patch dir; do
      if git -C "$ROOT/$dir" apply --reverse --check "$PATCH_DIR/$patch" 2>/dev/null; then
        git -C "$ROOT/$dir" apply --reverse "$PATCH_DIR/$patch"
        echo "[vendor-patches] reverted $patch"
      fi
    done
    ;;
  verify)
    entries | while read -r patch dir; do
      [ -f "$PATCH_DIR/$patch" ] || die "series names a missing patch: $patch"
      git -C "$ROOT/$dir" apply --reverse --check "$PATCH_DIR/$patch" 2>/dev/null ||
        die "$patch is not applied in $dir (run: make deps)"
    done
    for dir in $(entries | awk '{print $2}' | sort -u); do
      want=$(entries | awk -v d="$dir" '$2 == d {print $1}' | while read -r patch; do
               git -C "$ROOT/$dir" apply --numstat "$PATCH_DIR/$patch"
             done | shape)
      have=$(git -C "$ROOT/$dir" diff --numstat | shape)
      [ "$want" = "$have" ] || {
        echo "[vendor-patches] $dir differs from its pinned commit by more than its patches:" >&2
        echo "  patches: $want" >&2
        echo "  tree:    $have" >&2
        exit 1
      }
    done
    echo "[vendor-patches] OK: the vendored trees are the pinned upstream plus $(entries | wc -l | tr -d ' ') patch(es)"
    ;;
  *) die "unknown mode: $MODE" ;;
esac
