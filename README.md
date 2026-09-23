# SSH Gateway

A lightweight SSH bastion for managing servers, terminals, and remote files. Give AI tools or SSH clients separate relay credentials without sharing the target server's original password or private key.

- WebSSH terminals, remote file editing, uploads, and downloads.
- Server accounts, tags, user permissions, and source IP restrictions.
- Local and reverse port forwarding, diagnostics, and bulk import.
- Encrypted backups, an optional master password, and light/dark themes.

## Download and run

[Download](https://davidkeith2018.github.io/ssh-gateway/#downloads) a package for your system. No Go, Node.js, or compilation is required.

- **Windows portable:** extract the ZIP and run `ssh-gateway-desktop-windows.exe`.
- **Desktop installer:** install the app, open SSH Gateway, and set an administrator password.
- **Linux server:** extract the archive and run:

```bash
./ssh-gateway-server serve
```

Open `http://127.0.0.1:8080` and use the initial administrator password printed in the startup log. Add a server, verify its fingerprint, and configure relay credentials and allowed sources.

Windows desktop requires Microsoft WebView2. If it is already installed, no runtime installation is needed. If missing, run the included `MicrosoftEdgeWebview2Setup.exe` separately. App setup does not download or install WebView2 automatically.

The Linux server listens on localhost by default and stores data in `./data`. Keep the gateway, its data, and backups separate from systems that should only receive relay access.

[Website](https://davidkeith2018.github.io/ssh-gateway/) · [Quick start](https://davidkeith2018.github.io/ssh-gateway/#start) · [Platforms](https://davidkeith2018.github.io/ssh-gateway/#platforms)
