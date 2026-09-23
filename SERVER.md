# Linux Server

SSH Gateway runs as a standalone service with browser administration, WebSSH, SFTP, relay credentials, source restrictions, and connection logs. It needs no desktop environment, Go, Node.js, or external database at runtime.

File and folder favorites are stored in the gateway database, separately for each web user and SSH connection, and included in encrypted backups. Signing in to the same gateway from another browser restores them. Existing browser-local favorites are merged when that connection is opened; the local copy is removed only after a successful import.

## Separate deployment for AI access

Run the gateway on a separate machine whose files and administration are unavailable to the AI client. Give the client only the target's relay credentials and allow its source IP.

```text
AI client -> relay credentials + source restrictions -> SSH Gateway -> target server
                                                         |
                                               original target credentials
```

Keep gateway host access, administrator accounts, database, encryption keys, and backups private. Do not expose them through shared folders, mounts, or synchronization. Use target accounts with only the required privileges, and revoke or rotate relay credentials after use. Source rules and credential expiration are separate controls; another active source rule can still authorize a connection.

Administrator-password encryption protects locked credentials, but cannot prevent a privileged process from reading an unlocked application's memory. The gateway does not provide per-command approval.

## Run directly

Download and extract the Linux amd64 or arm64 server archive, then run inside its directory:

```bash
./ssh-gateway-server -data ./data -listen 127.0.0.1:2222 -web 127.0.0.1:8080 serve
```

Open `http://127.0.0.1:8080`. The initial administrator password is printed at startup. Put flags before `serve`.

For initial remote administration, use the server's existing SSH service to create a tunnel:

```bash
ssh -N -L 8080:127.0.0.1:8080 user@server-address
```

Open `http://127.0.0.1:8080` on your computer. Connections through the tunnel appear to come from the server's loopback address; allow `127.0.0.1` on the target. Use direct HTTPS when each browser's real source address must remain visible.

## Run with systemd

The included unit requires systemd 235 or later. Run these commands from the extracted package directory:

```bash
sudo install -m755 ssh-gateway-server /usr/local/bin/ssh-gateway-server
sudo install -m644 ssh-gateway-server.service /etc/systemd/system/ssh-gateway-server.service
# Preserve existing configuration during upgrades.
sudo test -f /etc/ssh-gateway-server.conf || sudo install -m644 ssh-gateway-server.conf /etc/ssh-gateway-server.conf
sudo systemctl daemon-reload
sudo systemctl enable --now ssh-gateway-server
sudo journalctl -u ssh-gateway-server -n 30
```

systemd allocates the service identity and manages persistent data at `/var/lib/ssh-gateway-server`. The service runs without a desktop or interactive login. The initial password appears only on first startup. Desktop and server instances need separate data directories and different listener ports if run together.

```bash
sudo systemctl status ssh-gateway-server
sudo systemctl restart ssh-gateway-server
sudo journalctl -u ssh-gateway-server -f
sudo systemctl disable --now ssh-gateway-server
```

## Remote access

Edit `/etc/ssh-gateway-server.conf` for your network:

```ini
GATEWAY_SSH_LISTEN=0.0.0.0:2222
GATEWAY_WEB_LISTEN=0.0.0.0:8443
GATEWAY_TLS_CERT=/var/lib/ssh-gateway-server/server.crt
GATEWAY_TLS_KEY=/var/lib/ssh-gateway-server/server.key
```

Use a certificate matching the service hostname. Keep the certificate and key in the service data directory with its existing owner; use `0600` for the private key. Restart, then open `https://gateway.example.com:8443`. Configure host/cloud firewall access and per-target source IP/CIDR restrictions.

The service uses the TCP peer address and does not trust `X-Forwarded-For`. Reverse proxies change the observed source; use the built-in HTTPS listener when WebSSH source restrictions must see real client addresses.

## Master password

Credentials are encrypted using your administrator password by default. After each restart, manually unlock through HTTPS or a loopback management address, then sign in. SSH relay authentication and automatic forwarding wait for unlocking; systemd startup does not unlock the vault. Signing out does not relock a running service.

Security encryption protects target credentials rather than all stored data. It uses the administrator password; changing that password while unlocked also updates encryption. Password reset cannot bypass locked encryption. Backups require the administrator password used when they were created. Protect older backups containing unprotected keys.

## Upgrade, backup, and removal

Stop the service and back up the complete `/var/lib/ssh-gateway-server` directory, including the database, encryption key, and SSH host key. Preserve permissions. A database-only backup cannot recover encrypted target passwords.

```bash
sudo systemctl stop ssh-gateway-server
sudo install -m755 ssh-gateway-server /usr/local/bin/ssh-gateway-server
sudo systemctl start ssh-gateway-server
```

To remove the program while retaining data and configuration:

```bash
sudo systemctl disable --now ssh-gateway-server
sudo rm /etc/systemd/system/ssh-gateway-server.service /usr/local/bin/ssh-gateway-server
sudo systemctl daemon-reload
```

## Build from source

Go 1.26+ and Node.js 22.12+ are needed only to build:

```bash
make server-linux
GATEWAY_ARCH=arm64 make server-linux
```

Packages in `dist/` contain the binary, systemd unit, sample configuration, this guide, and checksums.
