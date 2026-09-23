# Project Guidelines

Communicate with the user in Chinese. Use English for project documentation, commit messages, release notes, and product branding. The product name is **SSH Gateway** in every language. Preserve multilingual support in the application and website, and keep Chinese and English messages consistent.

## Project

SSH Gateway is a lightweight SSH bastion with Linux server and desktop modes. It gives clients separate relay credentials so target passwords and private keys can remain with the gateway administrator.

Features include server and account management, source IP restrictions, WebSSH terminals, SFTP, remote editing, notes, quick commands, local and reverse forwarding, diagnostics, bulk import, and encrypted backups. Web user authorization and SSH relay credentials are separate access mechanisms; test their permissions and revocation independently.

## Stack and directories

- Root Go module: SQLite, SSH/SFTP, HTTP/WebSocket, business logic, and tests.
- `cmd/ssh-gateway/`: CLI and server entry point.
- `web/`: Vue 3, TypeScript, Vite, xterm.js, Monaco Editor, and Node.js Playwright tests.
- `desktop/`: Wails desktop application in a separate Go module, sharing the backend and frontend.
- `localization/` and `web/src/locales/`: application messages.
- `site/`: static product website and screenshots using fictional demo data.
- `web/demo/`: isolated demo services and screenshot generation.
- `packaging/`, `scripts/`, `.github/workflows/`: packaging, build tools, and CI.
- User documentation: `README.md`, `SERVER.md`, `desktop/README.md`, and `site/README.md`.

## Build and run

Source builds require Go 1.26+, Node.js 22.12+, and npm. Run commands from the repository root unless noted otherwise.

| Task | Command |
| --- | --- |
| Build frontend and server | `make build` |
| Start a local server | `./bin/ssh-gateway serve` |
| Build web frontend | `npm --prefix web run build` |
| Build desktop frontend and bindings | `npm --prefix web run build:desktop` |
| Package Linux server | `make server-linux` |
| Build website | `python3 scripts/build-site.py` |
| Preview website | `python3 -m http.server 8090 --bind 127.0.0.1 --directory .build/site` |

Run `npm --prefix web ci` before the first standalone frontend build. `make build` includes dependency installation.

Defaults: Web `127.0.0.1:8080`, SSH `127.0.0.1:2222`, server data `./data`. The first administrator password is printed at startup; never copy it into documentation or commits.

Go embeds `web/dist`. Build the frontend before Go builds or tests that embed it, and avoid rewriting build output concurrently. Desktop builds require their platform dependencies. Regenerate bindings after desktop Go API changes; do not edit generated files under `web/src/bindings/`.

## Frontend and website

- Reuse official shadcn/ui components for standard UI controls. Inspect existing components first; use the official CLI or registry for missing components and retain source attribution and theme adaptations.
- This project uses Vue. Check framework compatibility before adding components. Do not present handwritten controls as official components or introduce another component system without discussing the gap.
- Compose business UI from shared components and use Tailwind for layout; do not create duplicate base control libraries.
- Preserve browser-language detection and explicit language preferences. English is the primary documentation and website source language.
- Update message catalogs for interface text changes and run `npm --prefix web run check:locales`. Check narrow screens, long labels, keyboard access, and both themes.
- The website is a separate static page. Keep content and translations consistent, use relative assets, and preserve deployment under a repository subpath.

## Security and data boundaries

- Check server-side authorization, target access, source restrictions, and revocation; hidden frontend controls are not authorization.
- Never place passwords, private keys, master passwords, or backup contents in logs, error responses, test reports, or commits. Use generated credentials or clearly fictional fixtures.
- Tests and demos use temporary databases, isolated ports, and separate directories. Never access real service data. Clean up processes and containers created for the task.
- For migrations, imports, backups, and credential changes, verify compatibility, rollback on failure, and effects on active connections.

## Verification

New or changed critical frontend interactions require updated Node.js Playwright tests and an actual test run. Verify modules incrementally; a successful build is not an interaction test.

| Task | Command |
| --- | --- |
| Go race checks and static analysis | `make test` |
| Full browser regression | `make browser-test` |
| One browser module after frontend build | In `web`: `npx playwright test tests/<module>.spec.ts` |
| Website language and links | After preview startup: `node site/tests/language.mjs` |
| Website downloads | `node site/tests/downloads.mjs` |
| Website README links | `node site/tests/readme-links.mjs` |
| Website gallery | `node site/tests/gallery.mjs` |
| Demo screenshots | `npm --prefix web run demo:screenshots` |
| Patch whitespace | `git diff --check` |

Install Playwright Chromium in `web` before browser tests, or set `GATEWAY_CHROMIUM`. Use `GATEWAY_BROWSER_PORT` for an isolated browser fixture port and `GATEWAY_SITE_URL` for the website preview. Demo serving and screenshot generation share a port and must not run simultaneously.

Keep screenshots, logs, and reports in ignored directories such as `.build`, `test-results`, or `playwright-report`. Report commands, actual results, and verification limits in the response. Describe manual checks and unverified native behavior when automation is insufficient; never claim unexecuted checks passed.

## Public repository and delivery

- Keep product code, automated tests, necessary usage instructions, and fictional demo screenshots.
- Do not commit development journals, internal plans, acceptance reports, local screenshots, personal domains or paths, real server details, or credentials. Do not recreate a `docs` directory for those materials.
- Respect `.gitignore`; keep local data, test output, and reference material out of commits.
- Update links in README, website, scripts, and tests when documentation moves or is removed.
- Do not upload archive branches or old release history when preparing a fresh repository. Retain any requested backup locally and push only the intended branch and version tags.
