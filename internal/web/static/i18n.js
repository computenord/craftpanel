// UI strings for ComputeBox Craftpanel. English and German.
"use strict";

const STRINGS = {
  en: {
    "app.tagline": "Self-hosted Minecraft management by ComputeBox",
    "nav.logout": "Sign out",
    "poweredBy": "powered by",

    "login.title": "Sign in",
    "login.username": "Username",
    "login.password": "Password",
    "login.submit": "Sign in",

    "setup.title": "Create your admin account",
    "setup.hint": "This panel has no users yet. The first account becomes the administrator.",
    "setup.password2": "Repeat password",
    "setup.mismatch": "Passwords do not match",
    "setup.submit": "Create account",

    "dash.title": "Servers",
    "dash.empty.title": "No servers yet",
    "dash.empty.text": "Create your first Minecraft server. The panel downloads everything for you.",
    "dash.new": "New server",

    "status.stopped": "Stopped",
    "status.starting": "Starting",
    "status.running": "Running",
    "status.stopping": "Stopping",
    "status.installing": "Installing",
    "status.install_failed": "Install failed",

    "create.title": "Create a new server",
    "create.name": "Server name",
    "create.type": "Software",
    "create.version": "Minecraft version",
    "create.memory": "Memory",
    "create.port": "Port",
    "create.portAuto": "automatic",
    "create.submit": "Create server",
    "create.loadingVersions": "Loading versions",

    "actions.start": "Start",
    "actions.stop": "Stop",
    "actions.restart": "Restart",
    "actions.kill": "Force stop",
    "actions.delete": "Delete",
    "actions.retryInstall": "Retry installation",

    "tabs.console": "Console",
    "tabs.files": "Files",
    "tabs.backups": "Backups",
    "tabs.settings": "Settings",

    "detail.address": "Address",
    "detail.uptime": "Uptime",
    "detail.copy": "Copy",
    "detail.copied": "Copied",
    "detail.installing": "Downloading server files",

    "console.placeholder": "Type a command, for example: say hello",
    "console.send": "Send",
    "console.autoscroll": "Autoscroll",
    "console.hint.stopped": "The server is stopped. Start it to see live output here.",

    "files.name": "Name",
    "files.size": "Size",
    "files.modified": "Modified",
    "files.upload": "Upload",
    "files.newFolder": "New folder",
    "files.folderPrompt": "Name of the new folder:",
    "files.download": "Download",
    "files.rename": "Rename",
    "files.renamePrompt": "New name:",
    "files.delete": "Delete",
    "files.deleteConfirm": "Delete \"{name}\"? This cannot be undone.",
    "files.edit": "Edit",
    "files.save": "Save",
    "files.empty": "This folder is empty",
    "files.uploaded": "File uploaded",
    "files.saved": "File saved",

    "eula.title": "Minecraft EULA",
    "eula.text": "Mojang requires every server owner to accept the Minecraft End User License Agreement before the server can start.",
    "eula.link": "Read the Minecraft EULA",
    "eula.accepted": "EULA accepted",
    "eula.notAccepted": "EULA not accepted yet",
    "eula.accept": "Accept EULA",
    "eula.banner.title": "Accept the Minecraft EULA to use your servers",
    "eula.banner.text": "Mojang requires every server owner to accept the Minecraft End User License Agreement. Until you do, a server cannot start.",
    "eula.acceptFor": "Accept for {name}",
    "eula.acceptAll": "Accept for all servers",
    "eula.required": "EULA required",

    "java.tooOld": "This server needs Java {need}, but the host has Java {have}.",
    "java.tooOldHost": "Java {have} is installed, but some servers need a newer Java. Minecraft 26.1 and newer requires Java 25.",
    "java.needs": "Needs Java {need}",

    "settings.title": "Server settings",
    "settings.name": "Server name",
    "settings.memory": "Memory (MB)",
    "settings.javaPath": "Java path",
    "settings.javaPathHint": "Leave empty to use the java command from PATH.",
    "settings.autostart": "Start this server automatically when the panel starts",
    "settings.save": "Save settings",
    "settings.saved": "Settings saved",

    "props.title": "server.properties",
    "props.hint": "Changes take effect after a server restart. Unknown keys are kept as they are.",
    "props.save": "Save properties",
    "props.saved": "Properties saved",
    "props.empty": "No properties yet. The file is created on the first server start.",

    "danger.title": "Danger zone",
    "danger.text": "Deleting a server removes its world and all files permanently.",
    "danger.confirm": "Type the server name to confirm:",
    "danger.delete": "Delete this server",

    "java.missing": "Java was not found on this host. Install a Java runtime, for example on Debian or Ubuntu: sudo apt install openjdk-21-jre-headless (Minecraft 26.1 and newer needs Java 25)",

    "error.invalid_credentials": "Wrong username or password",
    "error.rate_limited": "Too many failed attempts. Please wait a few minutes.",
    "error.unauthorized": "Your session has expired. Please sign in again.",
    "error.eula_required": "Accept the Minecraft EULA first.",
    "error.java_too_old": "The installed Java is too old for this Minecraft version.",
    "error.not_stopped": "Stop the server first.",
    "error.upstream": "Could not reach the download servers. Check the host's internet connection.",
    "error.generic": "Something went wrong",

    "console.filter": "Filter output",
    "console.download": "Download log",

    "players.label": "Players",
    "misc.disk": "Disk",

    "backup.create": "Create backup",
    "backup.busy": "Backup or restore running",
    "backup.empty": "No backups yet. The first one is just a click away.",
    "backup.restore": "Restore",
    "backup.restoreConfirm": "Restore \"{name}\"? The current world and all files will be replaced. This cannot be undone.",
    "backup.deleteConfirm": "Delete backup \"{name}\"?",
    "backup.started": "Backup started",
    "backup.restoreStarted": "Restore started",
    "backup.auto": "Automatic daily backup",
    "backup.time": "Time of day",
    "backup.keep": "Keep count",
    "backup.keepHint": "How many automatic backups to keep. Manual backups are never deleted automatically.",

    "settings.restartOnCrash": "Restart this server automatically if it crashes",

    "upgrade.title": "Change Minecraft version",
    "upgrade.hint": "Downloads the chosen server version and keeps the world and all files. Create a backup first. The server must be stopped.",
    "upgrade.button": "Change version",
    "upgrade.confirm": "Change this server to {version} now?",
    "upgrade.started": "Version change started",

    "access.title": "Players and permissions",
    "access.whitelistTitle": "Whitelist",
    "access.opsTitle": "Operators (OPs)",
    "access.enforce": "Only whitelisted players can join",
    "access.add": "Add",
    "access.placeholder": "Minecraft name",
    "access.empty": "No entries",
    "access.offlineHint": "This server runs in offline mode. Names are not checked against Mojang and get offline UUIDs.",

    "panel.title": "Panel settings",
    "panel.version": "Installed version",
    "panel.backupDir": "Backup directory",
    "panel.backupDirHint": "Absolute path, empty for the default inside the data directory. With the hardened systemd service, external paths must be added to ReadWritePaths (systemctl edit craftpanel).",
    "panel.updateAvailable": "Version {v} is available",
    "update.banner": "Craftpanel {v} is available. Update by running the install command again.",
    "update.dismiss": "Hide",

    "totp.title": "Two-factor authentication",
    "totp.on": "Two-factor authentication is enabled",
    "totp.off": "Two-factor authentication is off",
    "totp.enable": "Enable",
    "totp.disable": "Disable",
    "totp.setupHint": "Add this secret to your authenticator app (Aegis, Google Authenticator, 1Password), then confirm with a code from the app.",
    "totp.code": "6 digit code",
    "totp.confirm": "Confirm",
    "totp.loginCode": "Two-factor code",

    "error.totp_invalid": "Wrong two-factor code",
    "error.invalid_player": "This Minecraft name does not exist",
    "error.bad_name": "Invalid player name (2-16 letters, digits, underscore)",

    "misc.back": "Back",
    "misc.cancel": "Cancel",
    "misc.close": "Close",
    "misc.loading": "Loading",
    "misc.port": "Port",
    "misc.version": "Version",
    "misc.confirmStop": "Stop this server?",
    "misc.confirmKill": "Force stop this server? Unsaved world data may be lost.",
    "misc.confirmRestart": "Restart this server?"
  },
  de: {
    "app.tagline": "Selbst gehostetes Minecraft Management von ComputeBox",
    "nav.logout": "Abmelden",
    "poweredBy": "powered by",

    "login.title": "Anmelden",
    "login.username": "Benutzername",
    "login.password": "Passwort",
    "login.submit": "Anmelden",

    "setup.title": "Admin-Konto erstellen",
    "setup.hint": "Dieses Panel hat noch keine Benutzer. Das erste Konto wird zum Administrator.",
    "setup.password2": "Passwort wiederholen",
    "setup.mismatch": "Die Passwörter stimmen nicht überein",
    "setup.submit": "Konto erstellen",

    "dash.title": "Server",
    "dash.empty.title": "Noch keine Server",
    "dash.empty.text": "Erstelle deinen ersten Minecraft-Server. Das Panel lädt alles Nötige herunter.",
    "dash.new": "Neuer Server",

    "status.stopped": "Gestoppt",
    "status.starting": "Startet",
    "status.running": "Läuft",
    "status.stopping": "Stoppt",
    "status.installing": "Installiert",
    "status.install_failed": "Installation fehlgeschlagen",

    "create.title": "Neuen Server erstellen",
    "create.name": "Servername",
    "create.type": "Software",
    "create.version": "Minecraft-Version",
    "create.memory": "Arbeitsspeicher",
    "create.port": "Port",
    "create.portAuto": "automatisch",
    "create.submit": "Server erstellen",
    "create.loadingVersions": "Versionen werden geladen",

    "actions.start": "Starten",
    "actions.stop": "Stoppen",
    "actions.restart": "Neustart",
    "actions.kill": "Sofort beenden",
    "actions.delete": "Löschen",
    "actions.retryInstall": "Installation wiederholen",

    "tabs.console": "Konsole",
    "tabs.files": "Dateien",
    "tabs.backups": "Backups",
    "tabs.settings": "Einstellungen",

    "detail.address": "Adresse",
    "detail.uptime": "Laufzeit",
    "detail.copy": "Kopieren",
    "detail.copied": "Kopiert",
    "detail.installing": "Serverdateien werden heruntergeladen",

    "console.placeholder": "Befehl eingeben, zum Beispiel: say hallo",
    "console.send": "Senden",
    "console.autoscroll": "Autoscroll",
    "console.hint.stopped": "Der Server ist gestoppt. Starte ihn, um hier die Live-Ausgabe zu sehen.",

    "files.name": "Name",
    "files.size": "Größe",
    "files.modified": "Geändert",
    "files.upload": "Hochladen",
    "files.newFolder": "Neuer Ordner",
    "files.folderPrompt": "Name des neuen Ordners:",
    "files.download": "Herunterladen",
    "files.rename": "Umbenennen",
    "files.renamePrompt": "Neuer Name:",
    "files.delete": "Löschen",
    "files.deleteConfirm": "\"{name}\" wirklich löschen? Das kann nicht rückgängig gemacht werden.",
    "files.edit": "Bearbeiten",
    "files.save": "Speichern",
    "files.empty": "Dieser Ordner ist leer",
    "files.uploaded": "Datei hochgeladen",
    "files.saved": "Datei gespeichert",

    "eula.title": "Minecraft EULA",
    "eula.text": "Mojang verlangt, dass jeder Serverbetreiber die Minecraft-Endnutzer-Lizenzvereinbarung akzeptiert, bevor der Server starten kann.",
    "eula.link": "Minecraft EULA lesen",
    "eula.accepted": "EULA akzeptiert",
    "eula.notAccepted": "EULA noch nicht akzeptiert",
    "eula.accept": "EULA akzeptieren",
    "eula.banner.title": "Minecraft EULA akzeptieren, um deine Server zu nutzen",
    "eula.banner.text": "Mojang verlangt, dass jeder Serverbetreiber die Minecraft-Endnutzer-Lizenzvereinbarung akzeptiert. Solange das nicht geschehen ist, kann kein Server starten.",
    "eula.acceptFor": "Für {name} akzeptieren",
    "eula.acceptAll": "Für alle Server akzeptieren",
    "eula.required": "EULA nötig",

    "java.tooOld": "Dieser Server braucht Java {need}, auf dem Host läuft aber Java {have}.",
    "java.tooOldHost": "Installiert ist Java {have}, manche Server brauchen ein neueres Java. Minecraft 26.1 und neuer verlangt Java 25.",
    "java.needs": "Braucht Java {need}",

    "settings.title": "Server-Einstellungen",
    "settings.name": "Servername",
    "settings.memory": "Arbeitsspeicher (MB)",
    "settings.javaPath": "Java-Pfad",
    "settings.javaPathHint": "Leer lassen, um den java-Befehl aus PATH zu verwenden.",
    "settings.autostart": "Diesen Server automatisch starten, wenn das Panel startet",
    "settings.save": "Einstellungen speichern",
    "settings.saved": "Einstellungen gespeichert",

    "props.title": "server.properties",
    "props.hint": "Änderungen greifen nach einem Server-Neustart. Unbekannte Schlüssel bleiben erhalten.",
    "props.save": "Properties speichern",
    "props.saved": "Properties gespeichert",
    "props.empty": "Noch keine Properties. Die Datei wird beim ersten Serverstart erstellt.",

    "danger.title": "Gefahrenbereich",
    "danger.text": "Beim Löschen eines Servers werden Welt und alle Dateien endgültig entfernt.",
    "danger.confirm": "Gib den Servernamen zur Bestätigung ein:",
    "danger.delete": "Diesen Server löschen",

    "java.missing": "Java wurde auf diesem Host nicht gefunden. Installiere eine Java-Laufzeit, zum Beispiel unter Debian oder Ubuntu: sudo apt install openjdk-21-jre-headless (Minecraft 26.1 und neuer braucht Java 25)",

    "error.invalid_credentials": "Benutzername oder Passwort ist falsch",
    "error.rate_limited": "Zu viele Fehlversuche. Bitte warte ein paar Minuten.",
    "error.unauthorized": "Deine Sitzung ist abgelaufen. Bitte melde dich neu an.",
    "error.eula_required": "Akzeptiere zuerst die Minecraft EULA.",
    "error.java_too_old": "Das installierte Java ist zu alt für diese Minecraft-Version.",
    "error.not_stopped": "Stoppe den Server zuerst.",
    "error.upstream": "Die Download-Server sind nicht erreichbar. Prüfe die Internetverbindung des Hosts.",
    "error.generic": "Etwas ist schiefgelaufen",

    "console.filter": "Ausgabe filtern",
    "console.download": "Log herunterladen",

    "players.label": "Spieler",
    "misc.disk": "Speicher",

    "backup.create": "Backup erstellen",
    "backup.busy": "Backup oder Restore läuft",
    "backup.empty": "Noch keine Backups. Das erste ist nur einen Klick entfernt.",
    "backup.restore": "Wiederherstellen",
    "backup.restoreConfirm": "\"{name}\" wiederherstellen? Die aktuelle Welt und alle Dateien werden ersetzt. Das kann nicht rückgängig gemacht werden.",
    "backup.deleteConfirm": "Backup \"{name}\" wirklich löschen?",
    "backup.started": "Backup gestartet",
    "backup.restoreStarted": "Wiederherstellung gestartet",
    "backup.auto": "Automatisches tägliches Backup",
    "backup.time": "Uhrzeit",
    "backup.keep": "Anzahl behalten",
    "backup.keepHint": "So viele automatische Backups werden behalten. Manuelle Backups werden nie automatisch gelöscht.",

    "settings.restartOnCrash": "Diesen Server nach einem Absturz automatisch neu starten",

    "upgrade.title": "Minecraft-Version wechseln",
    "upgrade.hint": "Lädt die gewählte Serverversion herunter, Welt und Dateien bleiben erhalten. Erstelle vorher ein Backup. Der Server muss gestoppt sein.",
    "upgrade.button": "Version wechseln",
    "upgrade.confirm": "Diesen Server jetzt auf {version} umstellen?",
    "upgrade.started": "Versionswechsel gestartet",

    "access.title": "Spieler und Rechte",
    "access.whitelistTitle": "Whitelist",
    "access.opsTitle": "Operatoren (OPs)",
    "access.enforce": "Nur Spieler auf der Whitelist dürfen joinen",
    "access.add": "Hinzufügen",
    "access.placeholder": "Minecraft-Name",
    "access.empty": "Keine Einträge",
    "access.offlineHint": "Dieser Server läuft im Offline-Modus. Namen werden nicht gegen Mojang geprüft und bekommen Offline-UUIDs.",

    "panel.title": "Panel-Einstellungen",
    "panel.version": "Installierte Version",
    "panel.backupDir": "Backup-Verzeichnis",
    "panel.backupDirHint": "Absoluter Pfad, leer für den Standard im Datenverzeichnis. Beim gehärteten systemd-Dienst müssen externe Pfade in ReadWritePaths ergänzt werden (systemctl edit craftpanel).",
    "panel.updateAvailable": "Version {v} ist verfügbar",
    "update.banner": "Craftpanel {v} ist verfügbar. Update per erneutem Ausführen des Install-Befehls.",
    "update.dismiss": "Ausblenden",

    "totp.title": "Zwei-Faktor-Authentifizierung",
    "totp.on": "Zwei-Faktor-Authentifizierung ist aktiv",
    "totp.off": "Zwei-Faktor-Authentifizierung ist aus",
    "totp.enable": "Aktivieren",
    "totp.disable": "Deaktivieren",
    "totp.setupHint": "Trage dieses Secret in deine Authenticator-App ein (Aegis, Google Authenticator, 1Password) und bestätige mit einem Code aus der App.",
    "totp.code": "6-stelliger Code",
    "totp.confirm": "Bestätigen",
    "totp.loginCode": "Zwei-Faktor-Code",

    "error.totp_invalid": "Falscher Zwei-Faktor-Code",
    "error.invalid_player": "Diesen Minecraft-Namen gibt es nicht",
    "error.bad_name": "Ungültiger Spielername (2-16 Buchstaben, Zahlen, Unterstrich)",

    "misc.back": "Zurück",
    "misc.cancel": "Abbrechen",
    "misc.close": "Schließen",
    "misc.loading": "Lädt",
    "misc.port": "Port",
    "misc.version": "Version",
    "misc.confirmStop": "Diesen Server stoppen?",
    "misc.confirmKill": "Diesen Server sofort beenden? Nicht gespeicherte Weltdaten können verloren gehen.",
    "misc.confirmRestart": "Diesen Server neu starten?"
  }
};

let LANG = localStorage.getItem("cp_lang");
if (LANG !== "de" && LANG !== "en") {
  LANG = (navigator.language || "en").toLowerCase().startsWith("de") ? "de" : "en";
}

function t(key, vars) {
  let s = (STRINGS[LANG] && STRINGS[LANG][key]) || STRINGS.en[key] || key;
  if (vars) {
    for (const k of Object.keys(vars)) {
      s = s.replaceAll("{" + k + "}", vars[k]);
    }
  }
  return s;
}

function setLang(lang) {
  LANG = lang === "de" ? "de" : "en";
  localStorage.setItem("cp_lang", LANG);
  document.documentElement.lang = LANG;
}
