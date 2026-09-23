# Product Website

A standalone static website for SSH Gateway, deployed through GitHub Pages. English is the source HTML language; English and Chinese remain available through the language switcher. The management application runs on the user's own gateway, not on this website.

## Preview

From the repository root:

```bash
GITHUB_REPOSITORY=owner/repository python3 scripts/build-site.py
python3 -m http.server 8090 --bind 127.0.0.1 --directory .build/site
```

Open `http://127.0.0.1:8090`. No npm installation is needed for the website itself. Repository links are injected during the build and hidden when the repository is not configured.

## Publish

1. Push the project to GitHub.
2. In **Settings > Pages > Build and deployment > Source**, select **GitHub Actions**. Recreated repositories need this one-time setting again.
3. Push a change to README, `site/`, the website build script, or the Pages workflow on `main`/`master`, or run the Pages workflow manually.
4. Open the URL in the `github-pages` deployment environment.

A `Get Pages site failed` / `Not Found` error normally means the repository's Pages setting is missing. The ordinary workflow token cannot enable it in place of a repository administrator. Deployment uploads only `.build/site`.

## Content and languages

- `index.html`: English source content, download links, and quick start.
- `translations.js`: English and Chinese message catalogs.
- `main.js`: language selection, repository links, and command copying.
- `style.css`: layout, themes, and responsive styles.
- `screenshots/`: product screenshots made with fictional demo data.

Language selection follows the URL (`?lang=en` or `?lang=zh`), a saved preference, then the browser language. Explicit language links preserve section anchors and repository subpaths. The product name stays SSH Gateway in both languages.

Downloads use fixed asset names under `releases/latest/download/`. The release workflow publishes versioned files, fixed aliases, and SHA-256 checksums after all platform builds pass.

## Checks

Install development dependencies and Playwright Chromium in `web`, start the preview, then run:

```bash
node site/tests/language.mjs
node site/tests/readme-links.mjs
node site/tests/downloads.mjs
node site/tests/gallery.mjs
```

Set `GATEWAY_SITE_URL` for a different preview address or `GATEWAY_CHROMIUM` for an existing browser. Tests cover translations, saved preferences, URL overrides, disabled storage, links, downloads, copying, the gallery, and desktop/mobile layouts.

<a id="demo"></a>

## Demo and screenshots

Run a local demo with fictional data:

```bash
npm --prefix web ci
npm --prefix web run demo:serve
```

Open `http://127.0.0.1:19876`. Demo-only credentials are `ssh-admin` / `test-admin-password-only` and `demo-reader` / `demo-reader-password`. Data is recreated on startup. Stop with Ctrl+C.

Screenshot generation requires Docker and Playwright Chromium. Stop the lightweight demo first, then run:

```bash
cd web
npx playwright install chromium
npm run demo:screenshots
```

The script uses an isolated OpenSSH container, executes real commands, and does not mount host directories. Screenshots are written to `site/screenshots/`; the container is cleaned up afterward. Screenshots illustrate the product and are not a claim that every production or native interaction has been tested.
