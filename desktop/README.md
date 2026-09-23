# SSH Gateway Desktop

The desktop application uses Wails v3 (`v3.0.0-beta.20`) and shares the Go backend and Vue interface with the server. Running it does not require Go or Node.js. Chinese and English interfaces are available; the product name is SSH Gateway in both.

## Run

- **Windows portable:** extract the ZIP and run `ssh-gateway-desktop-windows.exe`.
- **Windows installer:** run the setup EXE and open SSH Gateway from its shortcut.
- **macOS:** open the DMG and copy SSH Gateway to Applications.
- **Ubuntu:** install the DEB with `sudo apt install ./ssh-gateway-desktop_<version>_amd64.deb`.

Windows requires Microsoft WebView2. If the runtime is present, use the app directly. App setup does not install or download it automatically. If missing, run the included `MicrosoftEdgeWebview2Setup.exe` separately; it requires internet access. Both the ZIP and the application installation directory include this helper.

On first launch, set an administrator password and optionally enable a master password. You can select an existing data directory instead. Add a server, verify its host fingerprint, and configure relay credentials and allowed sources. Local desktop terminal connections require the target to allow `127.0.0.1`.

Desktop settings control the SSH listener and remote access. The advertised connection address is used in copied connection commands; it does not change the listening interface.

## Windows, tray, and data

Closing the main window normally hides it to the tray while SSH relay connections and file tasks continue. Use **Quit application** to stop the app; unsaved work and active connections require confirmation. On Linux, a StatusNotifier/AppIndicator host is required for the tray. Without it, closing the window enters the quit flow rather than hiding an inaccessible window.

Signing out closes desktop terminals but does not stop the independently running SSH relay. An administrator can change the login password in settings, or use the CLI `admin-password` command against the same data directory to reset it.

| System | Default data directory |
| --- | --- |
| Windows | `%LOCALAPPDATA%\SSH Gateway` |
| macOS | `~/Library/Application Support/SSH Gateway` |
| Linux | `$XDG_DATA_HOME/ssh-gateway` or `~/.local/share/ssh-gateway` |

The portable ZIP uses the same default data directory as the installer. Data is not stored beside the executable. A selected custom directory is remembered. To change an initialized directory, quit and restart with `-data /path/to/data`. Keep the database, `master.key`, and `host.key` together. Only one service instance may use a directory at a time.

## Master password and separation

The optional master password protects target passwords and private keys, not the entire database. After a restart, unlock with the master password before signing in. Resetting the administrator password cannot recover an unknown master password. Protect backups created before enabling this protection as well.

For AI use, deploy the gateway on a separate system whose data and administrator access are unavailable to the AI. Relay credentials should grant only the required target access. See the [server deployment guide](../SERVER.md#separate-deployment-for-ai-access).

## Build

Run from the repository root:

```bash
npm --prefix web ci
npm --prefix web run build
npm --prefix web run build:desktop
```

Ubuntu requires GTK3 and WebKitGTK development packages:

```bash
sudo apt-get install libgtk-3-dev libwebkit2gtk-4.1-dev
go -C desktop build -tags 'desktop,production,gtk3' -o ../dist/ssh-gateway-desktop .
bash packaging/linux/build.sh
```

For Windows, generate native resources, then build:

```bash
go -C desktop run ./tools/winres
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go -C desktop build \
  -tags 'desktop,production' -ldflags '-H windowsgui' \
  -o ../dist/ssh-gateway-desktop-windows.exe .
```

GitHub Actions downloads and checks the signature of the Microsoft runtime bootstrapper, compiles the NSIS installer as UTF-8, verifies its title and installation completion, and creates a portable ZIP.

On macOS with Xcode command-line tools:

```bash
bash packaging/macos/build.sh arm64
# Use amd64 for Intel Macs.
```

The script produces `.app.zip` and `.dmg` files. Without `GATEWAY_MAC_SIGN_IDENTITY`, signing is ad hoc; it is not Developer ID signing or notarization.

The [desktop workflow](../.github/workflows/desktop.yml) produces test artifacts. The [release workflow](../.github/workflows/release.yml) builds official packages for `vX.Y.Z` tags and publishes a Release after all checks pass.

## Updates

The sidebar shows the running version and supports manual update checks. Release builds also check GitHub Releases at startup and every six hours. Updates provide a release-page link for a manual download; they are not installed automatically. Quit the application before replacing its files and back up the data directory first.
