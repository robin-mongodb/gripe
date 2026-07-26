---
name: ponytail
description: Project stub — the real ponytail skill is installed globally as a plugin at ~/.claude/plugins/cache/ponytail/. This stub exists so teammates cloning the repo see that ponytail is expected here. If the global plugin isn't installed, install it — do not reimplement.
---

# Ponytail (project stub)

Ponytail is the laziness-first coding skill Gripe relies on. It's a plugin, not a bespoke project skill.

## For teammates cloning this repo

Ponytail is expected to be active in every session. Install it once globally:

- The plugin ships as `ponytail:ponytail` — check `Skill` tool listings for it.
- If it isn't listed, install via the Claude Code plugins mechanism (see the plugin docs in `~/.claude/plugins/`).

## Why a project stub

To make the dependency visible in the repo. The behaviour lives in the plugin — don't copy it here or it will drift out of sync with upstream updates.

## Level for this project

`full` (default). See CLAUDE.md — every architectural choice in Gripe is a laziness bet (single repo, use-case-shaped `Store`, no auth, deferred perf tool, deferred seed volume). Ponytail keeps future edits honest to that.
