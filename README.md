# ComputeBox Craftpanel

A small, self-hosted web panel for creating and managing Minecraft servers on a single Linux host. Built and maintained by [ComputeBox](https://computebox.de).

One static binary, no database, no runtime dependencies except Java for the Minecraft servers themselves. The web UI is embedded in the binary and speaks English and German.

## Features

- Create servers in the browser: pick Vanilla or Paper, pick a Minecraft version, done. The panel downloads the server jar straight from Mojang or PaperMC and verifies the checksum.
- Knows which Java version each Minecraft release needs and refuses to start a server on a JVM that is too old, instead of leaving you with a cryptic exit code.
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

Which Java you need depends on the Minecraft version: 1.21.x runs on Java 21, while Minecraft 26.1 and newer requires Java 25. The panel reads the requirement from Mojang when it downloads a server and refuses to start one with too old a JVM, telling you which version it needs rather than letting the server die with a bare exit code.

Upgrading: run the same install command again. Uninstall (keeps server data):

```bash
curl -fsSL https://raw.githubusercontent.com/computenord/craftpanel/main/install/install.sh | sudo bash -s -- --uninstall
```

## Schnellstart (Deutsch)

Ein Befehl als root installiert das Panel als systemd-Dienst:

```bash
curl -fsSL https://raw.githubusercontent.com/computenord/craftpanel/main/install/install.sh | sudo bash
```

Danach `http://DEINE-SERVER-IP:8420` im Browser öffnen und das Admin-Konto anlegen. Java wird für die Minecraft-Server benötigt (`sudo apt install -y openjdk-21-jre-headless`). Achtung: Minecraft 26.1 und neuer verlangt Java 25, ältere Versionen laufen mit Java 21. Das Panel prüft das vor dem Start und sagt dir, welche Java-Version fehlt. Für Updates einfach den Install-Befehl erneut ausführen.

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

## Testing a build before publishing a release

The panel binary is self-contained, so you can run it straight from a shell on any Linux box:

```bash
scp dist/craftpanel-linux-amd64 test-host:/tmp/craftpanel
ssh test-host '/tmp/craftpanel -addr :8420 -data ~/craftpanel-test'
```

To exercise the installer itself (service user, systemd unit, hardening, Java detection) without a published release, serve the binary over HTTP and point `CRAFTPANEL_URL` at it:

```bash
# on your machine, inside dist/
python3 -m http.server 8000

# on the test host, with install.sh copied over
sudo CRAFTPANEL_URL=http://YOUR-IP:8000/craftpanel-linux-amd64 bash install.sh
```

Afterwards `sudo bash install.sh --uninstall` removes the service and binary again, keeping `/var/lib/craftpanel`.

## Manual install (copying files by hand)

The installer does nothing magic. If you would rather copy the binary over yourself, for example while the repo is still private, these are the same steps by hand.

Copy `dist/craftpanel-linux-amd64` to the host with whatever you like: `scp`, WinSCP, a USB stick. Then, as root on the host:

```bash
# 1. verify you copied it intact (compare against dist/SHA256SUMS)
sha256sum /tmp/craftpanel-linux-amd64

# 2. put the binary in place
install -m 755 /tmp/craftpanel-linux-amd64 /usr/local/bin/craftpanel

# 3. create the service user and its data directory
useradd --system --home-dir /var/lib/craftpanel --shell /usr/sbin/nologin craftpanel
mkdir -p /var/lib/craftpanel
chown -R craftpanel:craftpanel /var/lib/craftpanel
chmod 750 /var/lib/craftpanel

# 4. install the unit (copy install/craftpanel.service verbatim)
cp craftpanel.service /etc/systemd/system/craftpanel.service
systemctl daemon-reload
systemctl enable --now craftpanel

# 5. Java for the Minecraft servers
apt install -y openjdk-21-jre-headless
```

On RHEL and Alma the nologin shell is `/sbin/nologin`, adjust step 3 accordingly.

Check it came up with `systemctl status craftpanel` and `journalctl -u craftpanel -f`, then open `http://HOST:8420`.

To try it without systemd at all, just run the binary as your own user. It writes everything below the `-data` directory and needs no root:

```bash
./craftpanel-linux-amd64 -addr :8420 -data ~/craftpanel-test
```

Removing a manual install: `systemctl disable --now craftpanel`, then delete `/etc/systemd/system/craftpanel.service`, `/usr/local/bin/craftpanel` and, if you want the worlds gone too, `/var/lib/craftpanel`.

## Data layout

Everything the panel owns lives under the data directory:

```
/var/lib/craftpanel/
  users.json            accounts (argon2id hashes)
  sessions.json         active sessions, tokens stored hashed
  servers/
    my-server/
      server.json       panel config: type, version, port, memory, autostart
      data/             the Minecraft server itself
        server.jar
        eula.txt
        server.properties
        world/
```

That means backups are a plain copy of the data directory, and moving the panel to another host is copying it across. Stop the service first so no world is written mid-copy.

To adopt an existing Minecraft server, create a server in the UI with the matching version, stop it, then copy your old `world/`, `plugins/` and `server.properties` into that server's `data/` folder. Keep ownership right afterwards: `chown -R craftpanel:craftpanel /var/lib/craftpanel`.

## License and trademarks

Minecraft is a trademark of Mojang Synergies AB. This project is not affiliated with Mojang or Microsoft. Accepting the Minecraft EULA is a decision of each server operator.

---

powered by [ComputeBox](https://computebox.de)
