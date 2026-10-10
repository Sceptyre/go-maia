# Memory Index

## Repo: sceptyre/go-maia (workspace mirror of github.com/sceptyre/go-maia)
- Go CLI (Cobra) for AI code-change planning; worktrees under `~/.maia/worktrees/<repo>/<slug>/`, status in YAML frontmatter of `.maia/{change.md,.generated/{research,plan}.md}`.
- Build: `export PATH=$PATH:/usr/local/go/bin && go build -o /tmp/maia .` — **Go 1.24.4 installed at /usr/local/go/bin, NOT on default PATH** (installed 2026-10-10 via go.dev tarball; sudo available).
- Pre-existing repo quirks: most `cmd/`+`internal/` files are NOT gofmt-clean (spaces not tabs) and `go vet ./...` fails on apply/init/steer (`fmt.Println` redundant newline) — don't "fix" as side effects, and never gofmt unrelated files.
- AI commands fail fast without API key (`~/.maia/config.json` or `OPENAI_API_KEY`); set `OPENAI_API_KEY=sk-dummy` to get multi-second runs for stream testing.

## Web UI (added 2026-10-10, commit "feat: maia web")
- `maia web [--port 7878] [--addr] [--token]` → `cmd/web.go` + `internal/web/` (server/handlers/jobs/render + embedded static SPA, vanilla JS, zero build step).
- Long ops run by **re-exec'ing `os.Executable()`** as subprocess (repo root as cwd), output line-streamed over SSE with replay (`?after=` / Last-Event-ID); one job at a time (409 on conflict), cancellable via `Process.Kill` → status "canceled".
- Auth gotcha: **token enforced only on `/api/` paths** — `<script>`/`<link>` tags can't carry `?token=`, so protecting static assets made the SPA's JS 401 and the page silently dead. JS prompts for token on first API 401.
- goldmark promoted indirect→direct for server-side markdown→HTML; frontmatter stripped, raw HTML left escaped (default, no `WithUnsafe`).
- Test recipe: temp git repo + `maia new` + `maia web`, curl JSON/SSE endpoints; browser-verify via 127.0.0.1. `browser_get_content` extraction caches/stales — trust `browser_get_state` for live DOM.
