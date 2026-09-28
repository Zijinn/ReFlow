#!/usr/bin/env bash
set -euo pipefail

test -f .github/workflows/release.yml
test -f build/darwin/Info.plist
test -f build/darwin/entitlements.plist
test -f build/windows/installer.nsi
test -f build/windows/wails.exe.manifest
test -f build/windows/info.json

version="$(node -p "require('./package.json').version")"
test "$version" = "$(node -p "require('./web/package.json').version")"
grep -q "default: $version" .github/workflows/release.yml

# The release workflow injects the tag version into the Windows resources with a literal string
# replace. If a committed literal drifts from package.json the replace matches nothing and the
# build ships a stale FileVersion, so every occurrence must agree.
test "$version" = "$(node -p "require('./build/windows/info.json').fixed.file_version")"
test "$version" = "$(node -p "require('./build/windows/info.json').info['0000'].ProductVersion")"
grep -q "version=\"$version\"" build/windows/wails.exe.manifest
grep -qF -- "!define VERSION \"$version\"" build/windows/installer.nsi
grep -qF -- ".Replace('$version', \$env:VERSION)" .github/workflows/release.yml
grep -qF -- ".Replace('version=\"$version\"'" .github/workflows/release.yml
grep -q "Version = \"$version\"" internal/version/version.go
grep -q "<string>$version</string>" build/darwin/Info.plist
test "$(grep -c "<string>$version</string>" build/darwin/Info.plist)" = "2"

ruby -ryaml -rjson -rrexml/document -e '
  workflow = YAML.load_file(".github/workflows/release.yml")
  expected_jobs = ["license-inventory", "macos-universal", "publish-release", "windows-x64"]
  abort "release workflow has unexpected jobs" unless workflow["jobs"].is_a?(Hash) && workflow["jobs"].keys.sort == expected_jobs
  YAML.load_file("api/openapi.yaml")
  JSON.parse(File.read("build/windows/info.json"))
  REXML::Document.new(File.read("build/darwin/Info.plist"))
  REXML::Document.new(File.read("build/darwin/entitlements.plist"))
  puts "release configuration parses"
'

grep -q 'lipo -create' .github/workflows/release.yml
grep -q 'makensis' .github/workflows/release.yml
grep -q 'notarytool submit' .github/workflows/release.yml
grep -q 'dist/ReFlow-${{ env.VERSION }}-macos-universal.dmg' .github/workflows/release.yml
grep -q 'dist/ReFlow-${{ env.VERSION }}-windows-x64-setup.exe' .github/workflows/release.yml
grep -q -- '-X github.com/Zijinn/ReFlow/internal/version.Version' .github/workflows/release.yml
grep -q './cmd/reflow-desktop' .github/workflows/release.yml
! grep -q '\.sha256' .github/workflows/release.yml
# Brand guard against a half-done rename: no legacy product name or legacy slug may survive
# in the workflow, including the old artifact pattern "pattern: <old name>-*".
! grep -Eq '[Aa]urora|[Cc]airn' .github/workflows/release.yml
