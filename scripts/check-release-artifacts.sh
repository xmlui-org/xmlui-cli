#!/usr/bin/env bash
#
# check-release-artifacts.sh — fail fast if install.sh and build.yml drift on
# release artifact filenames.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL_SH="${REPO_ROOT}/install.sh"
WORKFLOW="${REPO_ROOT}/.github/workflows/build.yml"

tmpdir="$(mktemp -d)"
cleanup() { rm -rf "${tmpdir}"; }
trap cleanup EXIT

install_artifacts="${tmpdir}/install.txt"
matrix_artifacts="${tmpdir}/matrix.txt"
checksum_artifacts="${tmpdir}/checksums.txt"
unix_matrix_artifacts="${tmpdir}/matrix-unix.txt"
unix_checksum_artifacts="${tmpdir}/checksums-unix.txt"

awk '
  /ARTIFACT="/ {
    line = $0
    sub(/^.*ARTIFACT="/, "", line)
    sub(/".*$/, "", line)
    if (line != "") print line
  }
' "${INSTALL_SH}" | sort -u > "${install_artifacts}"

awk '
  $1 == "label:"   { label = $2 }
  $1 == "archive:" { archive = $2 }
  label != "" && archive != "" {
    print "xmlui-" label "." archive
    label = ""
    archive = ""
  }
' "${WORKFLOW}" | sort -u > "${matrix_artifacts}"

awk '
  /for asset in / {
    line = $0
    sub(/^.*for asset in /, "", line)
    sub(/; do.*$/, "", line)
    n = split(line, a, /[[:space:]]+/)
    for (i = 1; i <= n; i++) {
      if (a[i] != "") print a[i]
    }
  }
' "${WORKFLOW}" | sort -u > "${checksum_artifacts}"

echo "install.sh artifacts:"
cat "${install_artifacts}"
echo

echo "workflow matrix artifacts:"
cat "${matrix_artifacts}"
echo

echo "workflow checksum artifacts:"
cat "${checksum_artifacts}"
echo

grep -v '^xmlui-windows-' "${matrix_artifacts}" > "${unix_matrix_artifacts}"
grep -v '^xmlui-windows-' "${checksum_artifacts}" > "${unix_checksum_artifacts}"

if ! diff -u "${install_artifacts}" "${unix_matrix_artifacts}"; then
  echo "release artifact drift: install.sh and Unix build matrix disagree" >&2
  exit 1
fi

if ! diff -u "${install_artifacts}" "${unix_checksum_artifacts}"; then
  echo "release artifact drift: install.sh and Unix publish-checksums disagree" >&2
  exit 1
fi

echo "release artifact precheck passed"
