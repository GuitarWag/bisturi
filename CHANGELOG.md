# Changelog

All notable changes to bisturi. Format follows [Keep a Changelog](https://keepachangelog.com);
versions follow [SemVer](https://semver.org). Per-release binaries and
auto-generated notes are on the [Releases page](https://github.com/GuitarWag/bisturi/releases).

## [Unreleased]

## [0.2.3] - 2026-07-12
### Added
- `--compact` (optional): replace cut blocks with a `claude -p` summary spliced
  in as an `isCompactSummary` message, reclaiming most tokens while keeping the
  gist. Hard cut stays the default; `--restore` drops the summary and restores
  the originals. Toggle it in the TUI with `c` (shown in the diff view).
- `ctrl+r` in the session picker reverses the order live.

## [0.2.2] - 2026-07-12
### Added
- `-r` / `--reverse` lists sessions oldest-first.
### Removed
- Python prototype (`attic/`); the repo is Go-only. Untracked `.DS_Store`.

## [0.2.1] - 2026-07-11
### Fixed
- Restore is idempotent — never duplicates transcript content when run twice or
  against an uncut file.
- `--list` and a cancelled picker exit 0 (were exit 1 on success).
- `--cut` rejects empty / inverted-range specs instead of silently rewriting.
- Restore preserves turn boundaries (inserts after the anchor turn's metadata).
### Security
- Session rewrites, backups, and surgery records keep `0600` perms (`0700` dir);
  they contain conversation text.
### Changed
- Renamed to **bisturi** (from ctx-bisturi): binary, `--version`, TUI header,
  slash command (`/bisturi`), and surgery dir (`~/.claude/bisturi/`).
- `--restore` writes in place with a backup and prints the restart notice.
- TUI: `y` from the list opens the diff review; only `y` in the diff applies.

## [0.2.0] - 2026-07-11
### Added
- Searchable session picker; run `bisturi` from any folder.
- `-s` matches the `/rename` name (what `claude --resume` shows), then id, then ai-title.
- TUI applies straight to the session file (the diff is the review).
- "Restart required" alert after applying and in the diff view.

## [0.1.0] - 2026-07-11
### Added
- Initial Go release: Bubble Tea TUI to cut topic blocks from a Claude Code
  session, safe file-order relinking, restorable surgeries, `/bisturi` slash
  command, and a GoReleaser release pipeline (linux/macOS/windows, amd64/arm64).

[Unreleased]: https://github.com/GuitarWag/bisturi/compare/v0.2.3...HEAD
[0.2.3]: https://github.com/GuitarWag/bisturi/compare/v0.2.2...v0.2.3
[0.2.2]: https://github.com/GuitarWag/bisturi/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/GuitarWag/bisturi/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/GuitarWag/bisturi/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/GuitarWag/bisturi/releases/tag/v0.1.0
