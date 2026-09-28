# NetNewsWire and Feedbin Reference Findings

Research focus: high-frequency RSS reading, keyboard shortcuts, context/menu actions, feed organization, and subscription management. Sources are primary product documentation, screenshots, and the official Feedbin API repository. Accessed 2026-07-18.

## NetNewsWire

### Product surface and information architecture

- The official home page describes NetNewsWire as a fast, stable, accessible reader for Mac, iPhone, and iPad. Its feature list explicitly includes easy keyboard navigation, single-key shortcuts, Starred, All Unread and Today smart feeds, hiding read articles/feeds, folders, search, multiple accounts, customizable Mac toolbar, multiple windows, and OPML import/export. This establishes a workflow centered on quickly clearing unread queues while preserving source hierarchy.
  - Source: https://netnewswire.com/
- The Mac screenshot page publishes dark and light screenshots. The iOS screenshot page labels separate Feeds list, articles list/timeline, article detail, and an iPad view with all three panes. The pane naming supports a conventional source-list -> timeline -> detail model, with source navigation kept visible on larger screens.
  - Mac screenshots: https://netnewswire.com/screenshots-mac-7.html
  - iOS/iPad screenshots: https://netnewswire.com/screenshots-ios-7.html

### High-frequency reading and keyboard model

- NetNewsWire's Mac help says most reading shortcuts are single keys without Command/Option/Control. Documented actions include `space` to scroll or go to next unread, `n`/`+` next unread, `r`/`u` toggle read, `k` mark all read, `o` mark older articles read, `l` mark all read and go to next unread, `m` mark unread and go to next unread, `s` toggle starred, and `b`/Enter open in browser.
- Source/section movement is also keyboard-first: `a`/`z` move to previous/next subscription; feed-list `,`/`.` collapse/expand and `;`/`'` collapse/expand all; arrows move focus between subscriptions, headlines/timeline, and detail. This is a useful model for a three-pane desktop reader where focus, not pointer position, controls the reading loop.
  - Source: https://netnewswire.com/help/mac/6.1/en/keyboard-shortcuts.html
- NetNewsWire's help index links explicit OPML import/export, Safari toolbar subscription, themes, and keyboard help. Subscription intake is therefore supported through both browser discovery and bulk migration, while management remains separate from article reading.
  - Source: https://netnewswire.com/help/mac/6.1/en/

### Context/menu evidence and limits

- The public help pages and feature list document command outcomes (star, read/unread, open, source movement, folder collapse/expand) but do not publish a detailed right-click/context-menu specification. The screenshots show the persistent feed/timeline/detail surfaces, not menus. Treat context menus as a place to expose the same item-level commands, rather than as a separately documented information architecture.

## Feedbin

### High-frequency reading and keyboard model

- Feedbin exposes its full shortcut list in-app with `?`. Navigation works with both arrow keys and Vim-style `j k h l`; `space` navigates through unread items; `s` stars; `m` toggles read/unread; `v` opens the original; `c` extracts full content; `f` opens sharing; `r` refreshes feeds; `/` searches; and `shift a` marks all read. Two-step shortcuts (`g` then `u/s/a`) jump to unread, starred, or all items. This is a compact, scan-friendly command language for repeated triage.
- Feedbin also assigns shortcuts to management while reading: `shift e` edits the selected feed, `e` expands/collapses a tag, `a` adds a subscription, `shift c` copies the selected article URL, `shift f` enters fullscreen, and Escape unfocuses a field. Management actions are reachable without leaving the current reading context.
  - Source: https://feedbin.com/help/keyboard-shortcuts/

### Reading surface and automation

- Feedbin's product page emphasizes a clean interface with customizable themes and typography, fullscreen reading, full-text extraction for partial feeds, actions that can automatically star/mark read/send push notifications, expressive search with saved searches, configurable sharing/read-later integrations, updated-article diffs, and newsletter ingestion. These are relevant patterns for a high-volume reader: reduce context switches, automate repetitive triage, and make a focused reading mode available.
  - Source: https://feedbin.com/
- The site describes Feedbin as a web-based RSS reader whose goal is “the purest RSS reading experience” and a destination to read above all. This supports prioritizing a fast article surface over a dashboard-heavy home screen.
  - Source: https://feedbin.com/help/

### Folders/tags and subscription management

- Feedbin models feed organization as tags/taggings rather than nested folders in its public API. `GET /v2/taggings.json` returns feed/tag pairs; POST creates a tagging; DELETE removes it. Tags can be renamed or deleted through `POST/DELETE /v2/tags.json`. A UI can therefore support one feed in multiple groups and lightweight rename/delete operations without moving the feed itself.
  - Taggings API: https://github.com/feedbin/feedbin-api/blob/master/content/taggings.md
  - Tags API: https://github.com/feedbin/feedbin-api/blob/master/content/tags.md
- Subscription management is explicit and reversible: list all subscriptions, get one by ID, create by feed URL or site URL, delete by ID, and PATCH a custom title. When a site exposes multiple feeds, creation returns choices; when a non-URL search string is passed, the API can return relevant search results. This suggests an add-subscription flow that accepts either a URL or a search phrase and handles ambiguity visibly.
  - Source: https://github.com/feedbin/feedbin-api/blob/master/content/subscriptions.md
- Saved searches are first-class objects with a name and query (for example, `javascript is:unread`) and can be listed, created, updated, deleted, and queried for matching entries. They are a useful alternative to adding more permanent folders for recurring slices such as unread-by-topic.
  - Source: https://github.com/feedbin/feedbin-api/blob/master/content/saved-searches.md

### Recent subscription-discovery and maintenance patterns

- Feedbin's official blog documents a Feed Search feature that finds feeds by name/partial name, author, or description; the feature also updates the subscription API so API clients get the same search results. This is a strong reference for subscription search that does not require users to know a feed URL.
  - Source: https://feedbin.com/blog/2025/07/29/feed-search/
- The official browser-extension post describes subscribing to feeds and saving pages to read later from Safari, Chrome, or Firefox. It also documents keyboard commands to open the extension and open the current article in a background tab, illustrating how capture can happen without disrupting reading.
  - Source: https://feedbin.com/blog/2025/07/30/browser-extension/
- Feedbin's fixable-feeds post describes continuous feed-health monitoring, alternatives for moved/broken feeds, and notices on the subscriptions page; OPML imports are checked for working alternatives too. This is a useful maintenance pattern for a subscription-management screen.
  - Source: https://feedbin.com/blog/2024/01/15/fixable-feeds/

## Product implications for ReFlow

- Preserve the three-pane reading loop: source scope on the left, chronological/unread timeline in the middle, and article detail on the right. Keep focus movement and unread progression usable from the keyboard.
- Adopt a small, discoverable single-key set for repeated actions (next unread, read/unread, star, mark-all-read, open original, search), plus a visible shortcut help surface modeled on Feedbin's `?` and NetNewsWire's Help > Keyboard Shortcuts.
- Treat folders and tags as different capabilities: nested folders can match NetNewsWire's hierarchy, while Feedbin's taggings suggest allowing a source to appear in multiple groups and supporting fast rename/delete.
- Make subscription add/search a single flow that accepts URL, site URL, or search text; show multiple-feed choices and preserve the current reading context after adding.
- Expose item commands in context menus/menus as parity with the keyboard actions. Public sources do not specify exact context-menu contents, so menu design should stay compact and mirror the documented commands rather than introduce hidden-only features.
- Add focused maintenance affordances (feed health, duplicate/moved-feed suggestions, OPML import/export) in subscription management, not in the main reading surface.
