# Roadmap des Forks petter1/icloud-mcp

Diese Datei gilt nur fuer den Fork. Sie soll reichen, damit eine Session ohne Vorwissen weiterarbeiten kann.

- Fork: https://github.com/petter1/icloud-mcp (`origin`)
- Upstream: https://github.com/ThomasCrouzet/icloud-mcp (`upstream`), MIT, Go >= 1.26, ein Hauptautor, aktiv (v1.1.0 vom 09.11.2026)
- Stand 18.11.2026 (abends): `upstream/main` bis v1.1.1 + Dependabot (go-imap beta.9, go-smtp 0.26.0) geprueft und eingemergt. Eigene Aenderungen: `create_mailbox` (#2) und `move_messages` (#4), siehe unten. Branch `claude/inspiring-cray-n2nrrt`, noch nicht in `main`
- Stand 19.11.2026: #1 (eigener IMAP/SMTP-Server pro Prozess) im Session-Branch `claude/nice-hamilton-yu3ghi` (baut auf PR #1 auf). GUI fuer Mailkonten liegt auf `feat/setup-ui`, wartet auf Entscheid zum Guard (siehe unten)
- Schwesterprojekt: https://github.com/petter1/orchard-mcp (TypeScript + Swift, macOS-Apps, siehe dort `ROADMAP.md`)

## Ziel und Rolle

icloud-mcp ist der iCloud-Zugang ohne Mac: Kalender ueber CalDAV, Kontakte ueber CardDAV, Mail ueber IMAP/SMTP. Der Fork wird lokal gebaut und verbessert. Hobbyprojekt: weitergearbeitet wird, wenn Tokens uebrig sind, in der Reihenfolge unten, ohne feste Termine.

**Entscheid 18.11.2026:** icloud-mcp wird auf Linux **als Backend fuer Orchard** eingebunden. Orchard (`petter1/orchard-mcp`) startet icloud-mcp als Kindprozess und reicht Kalender-, Kontakt- und Mail-Aufrufe weiter. Daraus folgt fuer diesen Fork:

- ~~Nah am Upstream bleiben.~~ **Aufgehoben 19.11.2026:** Kein aktives Upstreaming mehr, der Fork darf sich frei entwickeln (upstream kann sich Commits holen). Upstream-Updates, v. a. Sicherheitsfixes, weiterhin geprueft nachziehen; groessere Eigenentwicklungen machen das aufwendiger, das ist akzeptiert.
- **Mail-Tools so bauen, dass Orchard sie 1:1 abbilden kann.** Das betrifft `create_mailbox` und Batch-Move mit neuen UIDs. Gleiche Bedeutung wie die geplanten Orchard-Tools `mail_create_mailbox` und `mail_move`.
- Verworfen: den Code in Orchard uebernehmen (Go -> TypeScript). Dann fielen Upstream-Updates und Sicherheitsfixes weg.

## Arbeitsregeln fuer Sessions

- Diese Datei und Commit-Nachrichten muessen ASCII bleiben (Umlaute als ae/oe/ue), und Commits duerfen keinen `Co-Authored-By`-Trailer fuer KI-Tools tragen. Sonst schlaegt der CI-Schritt "public text policy" fehl.

- Go: Das Modul verlangt Go >= 1.26. Ist lokal ein aelteres Go installiert, laedt `go` die passende Toolchain selbst nach (`GOTOOLCHAIN=auto`, braucht Netz).
- Vor einem PR `make check` laufen lassen (gofmt, vet, Guards, Race-Tests, Protokoll-Evidenz, Coverage). Ein neues Tool beruehrt viele Stellen: `register.go` (`RegisteredTools`), Vertragstests in `internal/mcptools/contract_*_test.go` (Fixture, Fake-IMAP-Befehl, Pruefung), `register_test.go` (Formel), `docs_consistency_test.go` (Zaehler), README, `docs/architecture.md`, `docs/security.md`, `scripts/protocol_evidence.py`, `scripts/native_smoke.py`, CHANGELOG.

- Bauen/testen: `go build ./...`, `go test ./...`, Binary per `go install ./cmd/icloud-mcp`. Weitere Ziele im `Makefile`.
- Integrationstests gegen echtes iCloud brauchen Zugangsdaten. Die gibt es in Cloud-Sessions nicht. Dort nur Unit-Tests.
- **Zugangsdaten nie ins Repo, in Logs oder in den Chat.** Sie liegen auf dem Rechner in `~/.config/icloud-mcp/` (`email`, `mail_address`, `password`, je `chmod 600`, app-spezifisches Apple-Passwort) und werden vom Besitzer selbst angelegt.
- **Upstream nicht blind nachziehen.** Upstream mergt Dependabot-Updates automatisch. Vor dem Merge von `upstream/main` die neuen Commits und Abhaengigkeiten ansehen.
- **Keine Interaktion mit upstream (Entscheid Yanik 18.11.2026):** Claude oeffnet keine PRs, Issues oder Kommentare im Upstream-Repo und beteiligt sich nicht an Diskussionen dort. Upstream-PRs macht Yanik selbst. Claude bereitet hoechstens Branches im Fork vor (von `upstream/main` abgezweigt, ohne ROADMAP).
- Eine Aenderung pro Branch, abgezweigt vom Fork (Session-Branch bzw. `main`), Commits auf Englisch. Eigene Upstream-Branches sind nicht mehr noetig (Entscheid 19.11.).
- Security-Guards (`scripts/guards/*.go.txt`) nie eigenmaechtig lockern: Ausnahmen nur mit ausdruecklichem OK von Yanik.
- Nach jedem erledigten Schritt hier den Status nachfuehren ([x] + Datum).

## Stand Nutzung (Linux-Rechner, 13.11.2026)

- Release-Binary v1.1.0 (linux-amd64) liegt unter `~/.local/bin/icloud-mcp`. Pruefsumme ok, Build durch GitHub Actions des Upstreams. Die Sigstore-Signatur ist nicht selbst geprueft (kein `cosign`).
- In Claude Code als MCP `icloud` eingetragen, **nur lesend**: `ICLOUD_MCP_READ_ONLY=true`, Faehigkeiten `calendar.read,mail.read`, Kontakte aus, Zeitzone `Europe/Zurich`.
- Noch offen: Zugangsdaten anlegen, `--doctor` laufen lassen, Lesen testen.

## Code-Review 13.11.2026 (sicherheitsrelevante Teile)

- Passwoerter werden nur beim Start aus `file://`-Dateien gelesen: nur regulaere Dateien, Rechte <= 600, hoechstens 4 KiB. Fehlermeldungen enthalten weder Pfad noch Passwort. Logs laufen durch einen Redactor.
- Netzwerk nur zu Apple:
  - HTTPS-Allowlist: `caldav.icloud.com`, `contacts.icloud.com` + `pNN-`-Shards, Port 443, keine Redirects.
  - Mail fest auf `imap.mail.me.com:993` / `smtp.mail.me.com:587`, mit TLS-Pruefung.
  - Keine Telemetrie, kein `exec`.
- `READ_ONLY` entfernt alle Schreib-Tools zentral. Mail-Lesen nutzt `EXAMINE` + `PEEK`. Verschieben laeuft per IMAP `MOVE`.
- Risiko: kleines Projekt mit einem Autor und automatisch gemergten Dependabot-Updates. Deshalb aus dem Fork selbst bauen.

## Verbesserungen

| # | Luecke | Verbesserung | Phase |
|---|---|---|---|
| 1 | Mail nur iCloud (`imap.mail.me.com` fest im Code) | [x] 19.11.: `ICLOUD_MCP_IMAP_SERVER` / `ICLOUD_MCP_SMTP_SERVER`, ein Konto pro Prozess, der konfigurierte Server ist die einzige erlaubte Verbindung (nur DNS-Namen, TLS-Pruefung auf diesen Namen, SMTP nur STARTTLS, 465 abgelehnt). Login bei fremden Servern nur mit voller Adresse (sonst ein Fehlversuch pro Sitzung, fail2ban). Offen: gegen `mail.yodabyte.ch` testen. GUI: siehe #6 | 2 |
| 2 | Mail-Ordner lassen sich nicht anlegen | `create_mailbox` [x] 18.11. (Branch `feat/create-mailbox`) | 2 |
| 3 | Keine Reminders (neue iCloud-Reminders vermutlich nicht ueber CalDAV erreichbar) | Abklaeren, ob machbar. Sonst bleiben Reminders beim Mac/Orchard. Hinweis 18.11.: Das Upstream-README sagt selbst "Modern Reminders are not exposed by this CalDAV connector". | 3 |
| 4 | `move_message` nur einzeln | Batch-Move mit Rueckgabe der neuen UIDs: `move_messages` [x] 18.11. (Branch `feat/move-messages`) | 2 |
| 5 | Release-Binary aus fremder CI | Eigener Build aus dem Fork | 1 |
| 6 | Mailkonten von Hand per Umgebungsvariablen einrichten ist muehsam | Schlankes GUI `icloud-mcp-setup` (Wunsch Yanik 19.11.): lokale Webseite auf 127.0.0.1 mit Einmal-Token, legt pro Konto `~/.config/icloud-mcp/accounts/<id>/` an (Passwort nur schreiben, Dateien 600) und zeigt den `claude mcp add-json`-Befehl. Code + Tests auf Branch `feat/setup-ui`. **Blockiert:** Der Produktions-Guard verbietet Dateizugriffe in allem Nicht-Test-Code; das Setup-Tool braucht eine eng begrenzte Ausnahme (nur `internal/accounts`, `cmd/icloud-mcp-setup`, plus Regel "Server importiert `internal/accounts` nie"). Braucht Yaniks OK. | 2 |

## Reihenfolge (ohne Termine)

| Schritt | Inhalt | Wo | Status |
|---|---|---|---|
| 1 Setup | Fork [x] 13.11.; upstream geprueft nachziehen [x] 18.11.; `make check` gruen [x] 18.11.; Zugangsdaten anlegen, `--doctor`, Lesen testen; eigener Build (#5), Claude-Config auf eigenen Build umstellen | Linux-Rechner | offen |
| 2 Mail | #2 [x] 18.11., #4 [x] 18.11., #1 [x] 19.11. (nur Unit-Tests); #6 GUI (wartet auf Guard-Entscheid) | Cloud/Linux | teilweise |
| 3 Mail live testen | Testordner anlegen, 2-3 Mails hin und zurueck verschieben (iCloud); `mail.yodabyte.ch` als eigenes Konto (#1); danach `mail.mutate` freischalten (Yaniks OK) | Linux-Rechner | offen |
| 4 Abklaeren | #3 Reminders | ueberall | sehr wahrscheinlich nicht machbar |
| 5 Abschluss | Tests, README, CHANGELOG | ueberall | offen |
| 6 Anbindung Orchard | Zusammen mit Orchard Schritt 3/7: Orchard startet icloud-mcp als Kindprozess (stdio), Tool-Zuordnung siehe `ROADMAP.md` im Orchard-Fork. Mehrere Mailkonten = mehrere Kindprozesse. | Cloud/Linux | offen |

Schritt 1 ist Voraussetzung fuer den Live-Betrieb von Orchard auf Linux.

## Neue Mail-Tools (18.11.2026)

Beide gehoeren zur Gruppe `mail.mutate` und fallen bei `ICLOUD_MCP_READ_ONLY=true` weg.

- **`create_mailbox`** (`name`): legt genau einen Ordner an. Gibt es ihn schon (waehlbar), kommt `created=false` ohne CREATE. Abgelehnt werden INBOX, `*`/`%`, Leerzeichen am Rand, Namen, die sich nur in Gross-/Kleinschreibung von einem bestehenden unterscheiden, und verschachtelte Namen ohne vorhandenen Elternordner. Danach wird per LIST geprueft (`resultIncomplete`, falls der Ordner fehlt). Orchard-Gegenstueck: `mail_create_mailbox`.
- **`move_messages`** (`mailbox`, `uid_validity`, `uids` 1-50, `destination`): gleiche Regeln wie `move_message` (kein Trash/Junk als Ziel). Prueft vor dem ersten Move, dass alle UIDs existieren, verschiebt dann einzeln in der angegebenen Reihenfolge und liefert pro UID die neue `destinationUid`. Stoppt beim ersten Fehler: ohne bisherigen Move als Fehler, sonst `complete=false` mit `failed` und `notAttempted`. Orchard-Gegenstueck: `mail_move` (Batch).

Naechste Schritte dazu:
1. Auf dem Linux-Rechner gegen echtes iCloud testen (Testordner anlegen, 2-3 Mails hin und zurueck verschieben), erst danach `mail.mutate` freischalten.
2. Upstream-PRs: Die Branches `feat/create-mailbox` und `feat/move-messages` sind dafuer vorbereitet (von `upstream/main` abgezweigt, ohne ROADMAP). Oeffnen macht Yanik selbst, nicht Claude.


## Mehrere Mailkonten (19.11.2026)

Ein icloud-mcp-Prozess bedient genau ein Mailkonto. Fuer weitere Konten (z. B. `@razzledazzlerecords.ch`, `yfuchs@yodabyte.ch` auf `mail.yodabyte.ch`) je einen eigenen MCP-Server eintragen, nur mit Mail-Rechten, damit keine iCloud-Zugangsdaten noetig sind:

```
ICLOUD_MCP_ENABLE_MAIL=true
ICLOUD_MCP_PERMISSIONS=mail.read
ICLOUD_MCP_READ_ONLY=true
ICLOUD_MCP_IMAP_SERVER=mail.yodabyte.ch
ICLOUD_MCP_SMTP_SERVER=mail.yodabyte.ch
ICLOUD_MAIL_ADDRESS=file:///home/<user>/.config/icloud-mcp/accounts/<id>/mail_address
ICLOUD_MAIL_PASSWORD=file:///home/<user>/.config/icloud-mcp/accounts/<id>/password
```

Namen der MCP-Server: `mail-<id>` (z. B. `mail-yodabyte`). Das GUI (#6) erzeugt genau diese Eintraege. Vorher pruefen: `icloud-mcp --doctor` mit derselben Umgebung.
