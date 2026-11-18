#!/usr/bin/env bash
# Restore the sibling layout used by go.mod and scripts/build-web.mjs in a fresh CI workspace.
set -euo pipefail
cd "${GITHUB_WORKSPACE:?}"

checkout() {
  local path="$1" revision="$2"
  if [[ -e "$path" ]]; then
    echo "Dependency checkout already exists: $path" >&2
    return 1
  fi
  git init --quiet "$path"
  git -C "$path" fetch --quiet --depth=1 "https://github.com/CMA2401PT/${path##*/}.git" "$revision"
  git -C "$path" checkout --quiet --detach FETCH_HEAD
}

checkout neomega-r2             afce620c625a893b227696f1655be0ac73e882b3
checkout neomega-celler         54cd8bb0824cc4d7ad72e61f84c6386cd51b44a2
checkout neomega-blocks         9e6d41fb90ede80d51e16135c177a8c14757a93d
checkout neomega-nbt            0cc62741e754672e8255ead4eda4e4456ca1129d
checkout neomega-packfs         17da3f3e57547c24578d47dbd34077b1ad9468bd
checkout neomega-converter-core b68da0a2ccd1cb94d655d62f1a74428f0462d2fe
checkout apps/neomega-converter f9fd67bd0853841bafc2adbacd261e2ef360a6d2

# The published converter still imports the old fixer. Apply the same Go changes as
# converter commit 8820e63 until that revision is available upstream.
git -C apps/neomega-converter apply --unidiff-zero ../neomega-viewer-r2/.github/release/converter-fixer.patch
