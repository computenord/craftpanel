# ComputeBox Craftpanel

A small, self-hosted web panel for creating and managing Minecraft servers on a single Linux host. Built and maintained by [ComputeBox](https://computebox.de).

One static binary, no database, no runtime dependencies except Java for the Minecraft servers themselves. The web UI is embedded in the binary and speaks English and German.

## Features

- Create servers in the browser: pick Vanilla or Paper, pick a Minecraft version, done. The panel downloads the server jar straight from Mojang or PaperMC and verifies the checksum.
- Run several servers on one host, each with its own port, memory limit and Java path.
- Live console with command input, streamed over SSE.
- File manager: browse, upload, download, rename, delete and edit files, safely jailed to the server directory.
- One click Minecraft EULA acceptance and a server.properties editor.
- Autostart servers when the panel starts, graceful stop on shutdown.
- Login with argon2id password hashing, rate limited sign in, strict cookies, CSRF protection and hardened systemd sandboxing.

## Install (Linux, systemd)

One command as root:

```bash
curl -fsSL https://raw.githubusercontent.com/computenord/craftpanel/main/install/install.sh | sudo bash
```

Then open `http://YOUR-SERVER-IP:8420` and create your admin account. That is all.

Minecraft servers need Java on the host (the installer reminds you if it is missing):

```bash
sudo apt install -y openjdk-21-jre-headless
```

Upgrading: run the same install command again. Uninstall (keeps server data):

```bash
curl -fsSL https://raw.githubusercontent.com/computenord/craftpanel/main/install/install.sh | sudo bash -s -- --uninstall
```

## Schnellstart (Deutsch)

Ein Befehl als root installiert das Panel als systemd-Dienst:

```bash
curl -fsSL https://raw.githubusercontent.com/computenord/craftpanel/main/install/install.sh | sudo bash
```

Danach `http://DEINE-SERVER-IP:8420` im Browser öffnen und das Admin-Konto anlegen. Java wird für die Minecraft-Server benötigt (`sudo apt install -y openjdk-21-jre-headless`). Für Updates einfach den Install-Befehl erneut ausführen.

## Usage notes

- Data lives in `/var/lib/craftpanel` (or `~/.craftpanel` when run manually). Each server is a folder under `servers/`, the world and all files are in its `data/` subfolder.
- Panel port and data directory are flags: `craftpanel -addr :8420 -data /var/lib/craftpanel`.
- Forgot the password? `echo 'newpass' | sudo -u craftpanel -- /usr/local/bin/craftpanel -data /var/lib/craftpanel reset-password youruser` (the running panel picks the new password up immediately, no restart needed)
- Behind a reverse proxy, add `-behind-proxy` to the `ExecStart` line so the login rate limiter sees the real client IP instead of the proxy's.
- Logs: `journalctl -u craftpanel -f`

## Security

- The panel serves plain HTTP. For access over the internet put it behind a reverse proxy with TLS (Caddy, nginx, Traefik) and keep the panel port firewalled. Behind a TLS proxy the session cookie is automatically marked Secure via `X-Forwarded-Proto`.
- Sessions are random 256 bit tokens stored hashed on disk, valid for 7 days.
- Login attempts are rate limited per IP (8 failures per 15 minutes).
- The file manager uses kernel level path jailing (`os.Root`), so neither `..` traversal nor symlinks can escape a server's directory.
- Minecraft servers are started directly (no shell), with `log4j2.formatMsgNoLookups=true` as defense in depth for old versions.

## Building from source

Requires Go 1.25+ and Node (only once, to fetch the embedded UI fonts):

```bash
node scripts/fetch-fonts.mjs   # one time
go build -o craftpanel .       # local build
./build.sh                     # release binaries for linux/amd64, linux/arm64, windows/amd64
```

Releases are plain binaries named `craftpanel-linux-amd64` and `craftpanel-linux-arm64`, attached to GitHub releases. The installer downloads exactly these names.

## License and trademarks

Minecraft is a trademark of Mojang Synergies AB. This project is not affiliated with Mojang or Microsoft. Accepting the Minecraft EULA is a decision of each server operator.

---

powered by [ComputeBox](https://computebox.de)
