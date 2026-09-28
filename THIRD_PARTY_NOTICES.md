# Third-party provenance policy

## MrRSS

Source: `../MrRSS-main`

Upstream: https://github.com/WCY-dt/MrRSS

License: GNU General Public License version 3

ReFlow selectively refactors feed parsing, scheduling, SQLite persistence, REST behavior, RSSHub integration, OPML behavior, synchronization, and AI provider concepts from MrRSS. Ported code must retain upstream copyright and license notices.

## Folo

Source: `../Folo-dev`

Upstream: https://github.com/RSSNext/Folo

License: GNU Affero General Public License version 3

Folo is used only as a product, information architecture, and interaction reference. ReFlow must not copy Folo source files, generated SDK types, assets, or translations.

## Fluent Reader

Source: `../fluent-reader-master`

Upstream: https://github.com/yang991178/fluent-reader

License: BSD 3-Clause

ReFlow may port sync adapter and interaction implementations when useful. Ported code must retain the BSD copyright notice and disclaimer.

## Dependency notices

Release builds generate a dependency license inventory for Go and JavaScript packages before an artifact is uploaded. The cloud workflow uses `go-licenses report ./...` for Go modules and `pnpm licenses list --json` for the web workspace; the raw reports are retained as release artifacts.

The release owner must review both reports for every version. Dependencies with licenses incompatible with GPL-3.0-only are rejected before release. The inventory is evidence of the exact dependency graph used by the artifact, not a replacement for the notices above.
