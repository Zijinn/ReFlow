# ReFlow release checklist

Native installers are built only on GitHub-hosted runners. The development workstation is intentionally limited to frontend production builds, Go tests, vet, race checks, and desktop-tag compile checks.

## Before tagging

- Run `go test ./... -count=1`, `go test -race ./...`, and `go vet ./...`.
- Run `pnpm --dir web typecheck`, `pnpm --dir web lint`, `pnpm --dir web test -- --run`, `pnpm --dir web build`, and `pnpm --dir web build:desktop`.
- Run `bash scripts/check-release-config.sh` and review the OpenAPI route coverage test.
- Perform browser checks at 1440x900, 820x1180, and 390x844. Record no console errors, no horizontal overflow, and a working mobile reader back transition.
- Export a backup from the release candidate and restore it into a fresh database. Keep the database and `master.key` together; encrypted sync and AI credentials cannot be recovered from the JSON backup with a different key.
- Review `THIRD_PARTY_NOTICES.md` and the generated Go/JavaScript license reports.

## GitHub secrets

macOS signing and notarization use `MACOS_CERTIFICATE` (base64 PKCS#12), `MACOS_CERTIFICATE_PASSWORD`, `MACOS_SIGNING_IDENTITY`, `APPLE_ID`, `APPLE_APP_PASSWORD`, and `APPLE_TEAM_ID`. Without these secrets the workflow produces an ad-hoc signed DMG for internal testing; it is not a distributable notarized build.

Windows signing uses `WINDOWS_CERTIFICATE` (base64 PFX) and `WINDOWS_CERTIFICATE_PASSWORD`. Without them the workflow produces an unsigned NSIS installer for internal testing.

## Cloud artifact verification

- The macOS artifact is a universal arm64/x86_64 DMG. Verify with `lipo -archs` and `codesign --verify --deep --strict`.
- The Windows artifact is a per-user x64 NSIS installer. Install on a clean Windows 10/11 runner, confirm WebView2 bootstrap behavior, launch ReFlow from the installed path, and verify that uninstall leaves user data untouched. The installer must contain `web/dist` beside `ReFlow.exe`.
- If notarized, verify with `spctl --assess --type execute` and `xcrun stapler validate`.
- Publish only the DMG and EXE. GitHub supplies source archives automatically; retain license inventories as internal workflow artifacts.

## Recovery and rollback

- Keep the previous installer available until the new version passes clean-install and migration checks.
- A failed migration must stop startup without deleting the previous database. Restore the last known-good database and its matching `master.key` before retrying.
- Revoke any test device tokens and remove temporary provider profiles before publishing a production backup.
