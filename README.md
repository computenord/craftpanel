<div align="center">

# ComputeBox Craftpanel

**Create and manage Minecraft servers from your browser. One binary, one command to install.**

[![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-Linux%20amd64%20%7C%20arm64-333)](#install)
[![No dependencies](https://img.shields.io/badge/dependencies-none-3ddc84)](#requirements)

*powered by [ComputeBox](https://computebox.de)*

</div>

---

Craftpanel is a small, self-hosted control panel for Minecraft servers. You point it at a Linux box, it gives you a web UI where you create servers, watch their console, edit their files and start or stop them.

It ships as a **single static binary** with the web UI embedded. There is no database to set up, no Node, no Python, no Docker. The only thing you install alongside it is a Java runtime, because that is what Minecraft servers run on.

The interface speaks **English and German** and switches at the click of a button.

## Screenshots

<!--
Drop the images into docs/screenshots/ and uncomment this block. See
docs/screenshots/HOWTO.md for the exact shots and how to take them.

|  |  |
| :--: | :--: |
| ![Dashboard](docs/screenshots/dashboard.png) | ![Console](docs/screenshots/console.png) |
| The dashboard, with the EULA gate | Live console with command input |
| ![Files](docs/screenshots/files.png) | ![Login](docs/screenshots/login.png) |
| The jailed file manager | Sign in |
-->

## Features

**Server management.** Pick Vanilla or Paper, pick a Minecraft version from the live list, and the panel downloads the server jar straight from Mojang or PaperMC, verifying the upstream checksum before it ever touches disk. Run as many servers on one host as it can carry, each with its own port, memory limit and Java path.

**No cryptic startup failures.** The panel knows which Java version each Minecraft release requires, because it reads that from Mojang alongside the download. If your JVM is too old it tells you so, instead of letting the server die with a bare `exit status 1`.

**Live console.** Server output streams into the browser over server-sent events, and you can type commands straight back into it. Warnings and errors are colour coded, the last 2000 lines are kept so you see what happened before you opened the tab.

**File manager.** Browse, upload, download, rename, delete and edit files in place. Every path goes through a kernel level jail, so neither `..` traversal nor a symlink can escape a server's directory.

**The boring but necessary parts.** One click to accept the Minecraft EULA, a `server.properties` editor that preserves your comments and unknown keys, autostart on boot, and a graceful shutdown that lets every world save before the process exits.

**Security that is actually there.** Passwords are hashed with argon2id, sign in is rate limited per IP, session tokens are random 256 bit values stored hashed on disk, every mutating request needs a custom header so cross-site requests cannot forge one, and the systemd unit runs the panel sandboxed as its own unprivileged user.

## Install

One command as root on any systemd Linux host:

```bash
curl -fsSL https://raw.githubusercontent.com/computenord/craftpanel/main/install/install.sh | sudo bash
```

Then open `http://YOUR-SERVER-IP:8420`. The first visit creates your admin account. That is the whole setup.

The installer downloads the binary for your architecture, creates an unprivileged `craftpanel` system user, writes a hardened systemd unit and starts the service.

### Schnellstart (Deutsch)

Ein Befehl als root installiert das Panel als systemd-Dienst:

```bash
curl -fsSL https://raw.githubusercontent.com/computenord/craftpanel/main/install/install.sh | sudo bash
```

Danach `http://DEINE-SERVER-IP:8420` im Browser öffnen und das Admin-Konto anlegen. Java wird für die Minecraft-Server gebraucht, siehe unten. Für ein Update führst du denselben Befehl noch einmal aus.

### Upgrading and uninstalling

Run the install command again to upgrade. It stops the service, swaps the binary and starts it back up, backing up your unit file first.

```bash
# removes the service and the binary, keeps /var/lib/craftpanel
curl -fsSL https://raw.githubusercontent.com/computenord/craftpanel/main/install/install.sh | sudo bash -s -- --uninstall
```

## Requirements

A Linux host on amd64 or arm64 with systemd, and a Java runtime for the Minecraft servers.

Which Java you need depends on the Minecraft version you run:

| Minecraft version | Java required |
| ----------------- | ------------- |
| 1.17.x | Java 16 |
| 1.18 up to 1.20.4 | Java 17 |
| 1.20.5 up to 1.21.x | Java 21 |
| 26.1 and newer | Java 25 |

```bash
# Debian and Ubuntu
sudo apt install -y openjdk-21-jre-headless

# RHEL, Alma, Rocky
sudo dnf install -y java-21-openjdk-headless
```

You never have to look this up. The panel reads the requirement from Mojang for the exact version you picked, shows it on the server card, and refuses to start a server whose Java is too old. If you need to run different Minecraft versions side by side, install several JDKs and set a per-server Java path in the server settings.

## Usage

### Creating a server

Click **New server**, give it a name, choose Paper or Vanilla and a version. The panel downloads the jar in the background and shows a progress bar. When it is done, accept the Minecraft EULA, which the dashboard will prompt you for, and hit Start.

Ports are assigned automatically starting at 25565, or you can pick your own. Remember to open that port in your firewall if players connect from outside.

### Command line

```
craftpanel [flags] [command]

Commands:
  serve                       run the panel (default)
  reset-password <username>   set a new password, read from stdin
  version                     print the version

Flags:
  -addr string     HTTP listen address (default ":8420")
  -data string     data directory (default "~/.craftpanel")
  -behind-proxy    trust X-Forwarded-For from a reverse proxy for rate limiting
```

Locked out? Reset a password without touching the service. The running panel picks the change up immediately, no restart needed:

```bash
echo 'newpassword' | sudo -u craftpanel -- /usr/local/bin/craftpanel \
  -data /var/lib/craftpanel reset-password youruser
```

Watch what the panel is doing:

```bash
systemctl status craftpanel
journalctl -u craftpanel -f
```

## Putting it behind TLS

The panel speaks plain HTTP on purpose. For anything reachable from the internet, put it behind a reverse proxy that terminates TLS, and keep port 8420 closed in your firewall.

With Caddy this is the entire config:

```caddy
craftpanel.example.com {
    reverse_proxy 127.0.0.1:8420
}
```

With nginx, remember that the console is a streaming endpoint:

```nginx
location / {
    proxy_pass http://127.0.0.1:8420;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_buffering off;        # the panel also sends X-Accel-Buffering: no
    proxy_read_timeout 1h;
}
```

Then add `-behind-proxy` to the `ExecStart=` line in `/etc/systemd/system/craftpanel.service`, so the login rate limiter sees the real client address instead of lumping every visitor into the proxy's IP. Behind a TLS proxy the session cookie marks itself `Secure` automatically.

## Data layout

Everything the panel owns lives under one directory, `/var/lib/craftpanel` for a service install:

```
/var/lib/craftpanel/
  users.json            accounts, argon2id hashes
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

A backup is therefore a plain copy of that directory, and moving to another host is an `rsync`. Stop the service first so no world is written mid copy.

To adopt an existing Minecraft server, create one in the panel with a matching version, stop it, copy your old `world/`, `plugins/` and `server.properties` into that server's `data/` folder, then fix ownership with `chown -R craftpanel:craftpanel /var/lib/craftpanel`.

## Security notes

- Sessions are 256 bit random tokens, stored only as hashes, valid for 7 days and invalidated on password change.
- Sign in is rate limited to 8 failures per IP per 15 minutes, and the number of concurrent password hashes is capped so a login flood cannot exhaust host memory.
- Every state changing request must carry an `X-Craftpanel` header, which browsers will not attach cross site, and cookies are `SameSite=Strict`.
- The file manager resolves every path inside an `os.Root` jail, enforced by the kernel rather than by string checks.
- Minecraft servers are launched directly, never through a shell, with `log4j2.formatMsgNoLookups=true` as defence in depth for pre 1.18.1 versions.
- The systemd unit runs with `ProtectSystem=strict`, `NoNewPrivileges`, a private `/tmp` and write access to nothing but its own data directory.

Found a security problem? Please report it privately through this repository's security advisories rather than opening a public issue.

## Manual install

The installer does nothing you cannot do yourself. Copy `craftpanel-linux-amd64` to the host by any means you like, then as root:

```bash
# verify the copy against dist/SHA256SUMS
sha256sum /tmp/craftpanel-linux-amd64

install -m 755 /tmp/craftpanel-linux-amd64 /usr/local/bin/craftpanel

useradd --system --home-dir /var/lib/craftpanel --shell /usr/sbin/nologin craftpanel
mkdir -p /var/lib/craftpanel
chown -R craftpanel:craftpanel /var/lib/craftpanel
chmod 750 /var/lib/craftpanel

cp install/craftpanel.service /etc/systemd/system/craftpanel.service
systemctl daemon-reload
systemctl enable --now craftpanel
```

On RHEL and Alma the nologin shell lives at `/sbin/nologin`, adjust accordingly.

To try it without systemd or root at all, just run the binary. It writes everything below `-data` and needs no privileges:

```bash
./craftpanel-linux-amd64 -addr :8420 -data ~/craftpanel-test
```

## Building from source

Requires Go 1.25 or newer, plus Node once to fetch the UI fonts that get embedded into the binary.

```bash
node scripts/fetch-fonts.mjs   # one time
go build -o craftpanel .       # local build
./build.sh                     # release binaries for linux/amd64, linux/arm64, windows/amd64
```

Releases are plain binaries named `craftpanel-linux-amd64` and `craftpanel-linux-arm64`. The installer looks for exactly those names.

### Testing a build before you publish a release

Run the binary straight from a shell on a test host, or exercise the installer itself without a published release by pointing it at any URL:

```bash
# serve the binary from your machine, inside dist/
python3 -m http.server 8000

# on the test host
sudo CRAFTPANEL_URL=http://YOUR-IP:8000/craftpanel-linux-amd64 bash install.sh
```

The installer also honours `CRAFTPANEL_REPO`, `CRAFTPANEL_VERSION` and `CRAFTPANEL_PORT`.

## Troubleshooting

**The server exits immediately with `exit status 1`.** Almost always the Java version. Newer panels catch this before launching and tell you which Java you need, so upgrade the panel if you see a raw exit code.

**"eula not accepted".** Accept the Minecraft EULA from the dashboard banner or the server's settings tab. Mojang requires every operator to agree to it.

**Players cannot connect.** Check that the server's port is open in your firewall, and that the port shown on the server card matches `server-port` in `server.properties`.

**Login says "too many failed attempts".** The rate limiter blocks an IP after 8 failures for 15 minutes. Wait, or reset the password from the command line.

**Everything else.** `journalctl -u craftpanel -f` shows what the panel is doing, and the in-browser console shows what the Minecraft server is doing.

## Scope

Craftpanel deliberately does a few things well rather than everything badly. It manages one host, from one admin account, over a web UI. There is no multi tenancy, no user roles, no scheduled backup engine, no RCON bridge and no billing. If you need that, you want a bigger panel.

## License and trademarks

Minecraft is a trademark of Mojang Synergies AB. This project is not affiliated with Mojang or Microsoft, and accepting the Minecraft EULA remains the decision of each server operator.

---

<div align="center">

Built and maintained by **[ComputeBox](https://computebox.de)**

</div>
