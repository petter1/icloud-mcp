# Roadmap des Forks petter1/icloud-mcp

Diese Datei gilt nur für den Fork. Sie soll reichen, damit eine Session ohne Vorwissen weiterarbeiten kann.

- Fork: https://github.com/petter1/icloud-mcp (`origin`)
- Upstream: https://github.com/ThomasCrouzet/icloud-mcp (`upstream`), MIT, Go ≥ 1.26, ein Hauptautor, aktiv (v1.1.0 vom 09.11.2026)
- Stand 20.11.2026: `upstream/main` (v1.2.0) geprüft, über den Branch `sync/upstream-v1.2.0` in `main` gemergt (ohne Konflikte, nicht gepusht). Go 1.27.4 per Homebrew installiert; `go build`, `go vet`, `go test ./...` und `go test -race ./...` grün. Binary per `go install` in `~/go/bin/icloud-mcp`.
- Schwesterprojekt: https://github.com/petter1/orchard-mcp (TypeScript + Swift, macOS-Apps, siehe dort `ROADMAP.md`)

## Ziel und Rolle

icloud-mcp ist der iCloud-Zugang ohne Mac: Kalender über CalDAV, Kontakte über CardDAV, Mail über IMAP/SMTP. Der Fork wird lokal gebaut und verbessert. Gute Änderungen gehen als PR an upstream.

**Entscheid 18.11.2026:** icloud-mcp wird auf Linux **als Backend für Orchard** eingebunden. Orchard (`petter1/orchard-mcp`) startet icloud-mcp als Kindprozess und reicht Kalender-, Kontakt- und Mail-Aufrufe weiter. Daraus folgt für diesen Fork:

- **Nah am Upstream bleiben.** Eigene Änderungen klein halten und möglichst upstream anbieten, damit `upstream/main` sauber nachgezogen werden kann. Kein Umbau auf Orchard-spezifische Formate.
- **Mail-Tools so bauen, dass Orchard sie 1:1 abbilden kann.** Das betrifft `create_mailbox` und Batch-Move mit neuen UIDs. Gleiche Bedeutung wie die geplanten Orchard-Tools `mail_create_mailbox` und `mail_move`.
- Verworfen: den Code in Orchard übernehmen (Go → TypeScript). Dann fielen Upstream-Updates und Sicherheitsfixes weg.

## Arbeitsregeln für Sessions

- Bauen/testen: `go build ./...`, `go test ./...`, Binary per `go install ./cmd/icloud-mcp`. Weitere Ziele im `Makefile`.
- Integrationstests gegen echtes iCloud brauchen Zugangsdaten. Die gibt es in Cloud-Sessions nicht. Dort nur Unit-Tests.
- **Zugangsdaten nie ins Repo, in Logs oder in den Chat.** Sie liegen auf dem Rechner in `~/.config/icloud-mcp/` (`email`, `mail_address`, `password`, je `chmod 600`, app-spezifisches Apple-Passwort) und werden vom Besitzer selbst angelegt.
- **Upstream nicht blind nachziehen.** Upstream mergt Dependabot-Updates automatisch. Vor dem Merge von `upstream/main` die neuen Commits und Abhängigkeiten ansehen.
- Eine Änderung pro Branch, abgezweigt vom Fork-`main`, Commits auf Englisch.
- **Keine Interaktion mit upstream (Entscheid Yanik 18.11.2026):** Claude öffnet keine PRs, Issues oder Kommentare im Upstream-Repo. **Kein aktives Upstreaming (19.11.2026):** Der Fork darf sich frei entwickeln; Upstream-Updates (v. a. Sicherheitsfixes) weiterhin geprüft nachziehen.
- Roadmap ohne feste Termine, nur Reihenfolge (Entscheid 19.11.2026).
- **GitHub immer aktuell (Entscheid 20.11.2026):** Jeder fertige Branch und jede Roadmap-Änderung sofort nach `origin` pushen. Vor neuer Arbeit `git fetch origin`, offene Fork-PRs und diese Roadmap auf `main` prüfen. Roadmap-Änderungen auf `main` nachführen, nicht nur im Session-Branch (sonst Doppelarbeit wie 18.–20.11.).
- Nach jedem erledigten Schritt hier den Status nachführen (✅ + Datum).

## Stand Nutzung (Linux-Rechner, 13.11.2026)

- Release-Binary v1.1.0 (linux-amd64) liegt unter `~/.local/bin/icloud-mcp`. Prüfsumme ok, Build durch GitHub Actions des Upstreams. Die Sigstore-Signatur ist nicht selbst geprüft (kein `cosign`).
- In Claude Code als MCP `icloud` eingetragen, **nur lesend**: `ICLOUD_MCP_READ_ONLY=true`, Fähigkeiten `calendar.read,mail.read`, Kontakte aus, Zeitzone `Europe/Zurich`.
- Noch offen: Zugangsdaten anlegen, `--doctor` laufen lassen, Lesen testen.

## Code-Review 13.11.2026 (sicherheitsrelevante Teile)

- Passwörter werden nur beim Start aus `file://`-Dateien gelesen: nur reguläre Dateien, Rechte ≤ 600, höchstens 4 KiB. Fehlermeldungen enthalten weder Pfad noch Passwort. Logs laufen durch einen Redactor.
- Netzwerk nur zu Apple:
  - HTTPS-Allowlist: `caldav.icloud.com`, `contacts.icloud.com` + `pNN-`-Shards, Port 443, keine Redirects.
  - Mail fest auf `imap.mail.me.com:993` / `smtp.mail.me.com:587`, mit TLS-Prüfung.
  - Keine Telemetrie, kein `exec`.
- `READ_ONLY` entfernt alle Schreib-Tools zentral. Mail-Lesen nutzt `EXAMINE` + `PEEK`. Verschieben läuft per IMAP `MOVE`.
- Risiko: kleines Projekt mit einem Autor und automatisch gemergten Dependabot-Updates. Deshalb aus dem Fork selbst bauen.

## Upstream-Review 20.11.2026 (v1.1.0 → v1.2.0)

- Keine neuen Netzziele (weiterhin nur `caldav.icloud.com`, `contacts.icloud.com`), kein `exec`, keine TLS-Ausnahmen.
- Neu `ICLOUD_MAIL_FROM` (optionaler Absender, z. B. Alias einer eigenen Domain). Wird wie die übrigen Zugangsdaten über `loadCredential` geladen und als volle Mail-Adresse geprüft.
- Abhängigkeiten: `go-imap/v2` beta.8 → beta.9, `go-smtp` 0.25.1 → 0.26.0, `golang.org/x/text` 0.43 → 0.44. CI auf Go 1.26.11 (Security-Fix). Actions weiterhin per SHA gepinnt.
- Sonst viele Korrekturen bei Kalender (Ganztagestermine, Wiederholungen, DST-Warnungen), Mail (MIME, CONDSTORE-Flags) und Kontakten (Paging).
- Tools v1.2.0: `list_calendars`, `search_events`, `get_event`, `create_event`, `update_event`, `delete_event`, `validate_event`, `find_free_slots`, `calendar_capabilities`, `list_address_books`, `search_contacts`, `get_contact`, `create_contact`, `update_contact`, `delete_contact`, `list_mailboxes`, `search_messages`, `get_message`, `move_message`, `trash_message`, `set_message_flags`, `send_message`, `reply_message`, `icloud_capabilities`. `create_mailbox` und Batch-Move fehlen upstream weiterhin (#2, #4), sind im Fork aber schon gebaut: Branches `feat/create-mailbox` und `feat/move-messages` (Cloud-Session 18.11., Fork-PR #1, Basis v1.1.1). Dazu `feat/custom-mail-server` (Fork-PR #2) und `feat/setup-ui` (WIP). Diese Branches vor neuer Arbeit prüfen, nicht neu bauen. Auf v1.2.0 nachgezogen ✅ 20.11.: `feat/create-mailbox-v1.2` und `feat/move-messages-v1.2` (je allein Build, Vet und Tests grün; zusammen gemergt gibt es noch Konflikte in Mail-Registrierung und Tests). `feat/custom-mail-server` und `feat/setup-ui` noch nicht nachgezogen.
- Build und Tests ✅ 20.11.2026 (Go 1.27.4, inkl. `-race`).

## Verbesserungen

| # | Lücke | Verbesserung | Phase |
|---|---|---|---|
| 1 | Mail nur iCloud (`imap.mail.me.com` fest im Code) | Weitere IMAP/SMTP-Konten konfigurierbar (eigene Domains). Allowlist pro Konto explizit, kein freies Host-Ziel. | 2 |
| 2 | Mail-Ordner lassen sich nicht anlegen | `create_mailbox` | 2 |
| 3 | Keine Reminders (neue iCloud-Reminders vermutlich nicht über CalDAV erreichbar) | Abklären, ob machbar. Sonst bleiben Reminders beim Mac/Orchard. | 3 |
| 4 | `move_message` nur einzeln | Batch-Move mit Rückgabe der neuen UIDs | 2 |
| 5 | Release-Binary aus fremder CI | Eigener Build aus dem Fork | 1 |

## Phasen

| Phase | Inhalt | Status |
|---|---|---|
| 1 Setup | Fork ✅ 13.11. · upstream geprüft und gemergt ✅ 20.11. · Go installiert, gebaut, getestet ✅ 20.11. · Zugangsdaten anlegen, `--doctor`, Lesen testen · eigener Build (#5), Claude-Config auf eigenen Build umstellen | begonnen 20.11.2026 |
| 2 Mail | #1, #2, #4 | noch nicht eingeplant |
| 3 Abklären | #3 Reminders | noch nicht eingeplant |
| 4 Abschluss | Tests, README, Upstream-PRs | noch nicht eingeplant |
| 5 Anbindung Orchard | Zusammen mit Orchard Phase 5: Orchard startet icloud-mcp als Kindprozess (stdio), Tool-Zuordnung siehe `ROADMAP.md` im Orchard-Fork. Hier höchstens Kleinigkeiten nötig, z. B. stabile, maschinenlesbare Antworten. | nach Orchard Phase 1–4, frühestens 2027 |

Phase 1 ist Voraussetzung für Orchard Phase 5.
