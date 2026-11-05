#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="$(tr -d '[:space:]' < "$root/VERSION")"

if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "VERSION must be a semantic version, got: $version" >&2
  exit 1
fi
if [[ "$version" != "0.1.0" ]]; then
  echo "release version is frozen at 0.1.0, got: $version" >&2
  exit 1
fi

package_version="$(node -p "require(process.argv[1]).version" "$root/frontend/package.json")"
if [[ "$package_version" != "$version" ]]; then
  echo "frontend/package.json version $package_version does not match VERSION $version" >&2
  exit 1
fi

lock_version="$(node -p "require(process.argv[1]).packages[''].version" "$root/frontend/package-lock.json")"
if [[ "$lock_version" != "$version" ]]; then
  echo "frontend/package-lock.json version $lock_version does not match VERSION $version" >&2
  exit 1
fi

if [[ "${GITHUB_REF_TYPE:-}" == "tag" && "${GITHUB_REF_NAME:-}" != "v$version" ]]; then
  echo "release tag must be v$version, got: ${GITHUB_REF_NAME:-}" >&2
  exit 1
fi

echo "version $version is consistent"
