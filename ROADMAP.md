# Roadmap des Forks petter1/icloud-mcp

Diese Datei gilt nur für den Fork. Sie soll reichen, damit eine Session ohne Vorwissen weiterarbeiten kann.

- Fork: https://github.com/petter1/icloud-mcp (`origin`)
- Upstream: https://github.com/ThomasCrouzet/icloud-mcp (`upstream`), MIT, Go ≥ 1.26, ein Hauptautor, aktiv (v1.1.0 vom 09.11.2026)
- Stand 20.11.2026: `upstream/main` (v1.2.0) geprüft, über den Branch `sync/upstream-v1.2.0` in `main` gemergt (ohne Konflikte). Go 1.27.4 per Homebrew installiert; `go build`, `go vet`, `go test ./...` und `go test -race ./...` grün. Binary per `go install` in `~/go/bin/icloud-mcp`.
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
- **Beide Forks gemeinsam weiterentwickeln (Entscheid 27.11.2026):** Jede neue Funktion oder Luecke in einem Fork auch fuer den anderen ansehen und dort in der ROADMAP eintragen (Orchard-Tool und icloud-mcp-Gegenstueck mit gleicher Bedeutung).
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
- Tools v1.2.0: `list_calendars`, `search_events`, `get_event`, `create_event`, `update_event`, `delete_event`, `validate_event`, `find_free_slots`, `calendar_capabilities`, `list_address_books`, `search_contacts`, `get_contact`, `create_contact`, `update_contact`, `delete_contact`, `list_mailboxes`, `search_messages`, `get_message`, `move_message`, `trash_message`, `set_message_flags`, `send_message`, `reply_message`, `icloud_capabilities`. `create_mailbox` und Batch-Move fehlen upstream weiterhin (#2, #4), sind im Fork gebaut. Stand Branches siehe «Verbesserungen» und «Branches» unten.
- Build und Tests ✅ 20.11.2026 (Go 1.27.4, inkl. `-race`).

## Verbesserungen

| # | Lücke | Verbesserung | Phase |
|---|---|---|---|
| 1 | Mail nur iCloud (`imap.mail.me.com` fest im Code) | Weitere IMAP/SMTP-Konten konfigurierbar (eigene Domains). Allowlist pro Konto explizit, kein freies Host-Ziel. | 2 · Code fertig (Cloud 19.11., Fork-PR #2): ein eigener Server pro Prozess (`ICLOUD_MCP_IMAP_SERVER`, `ICLOUD_MCP_SMTP_SERVER`). Auf v1.2.0 nachgezogen ✅ 20.11. als `feat/custom-mail-server-v1.2`, `make check` grün. **Wartet auf Yaniks Review**, weil es die erlaubten Netzziele ändert. GUI dazu: `feat/setup-ui` (WIP, nicht nachgezogen). |
| 2 | Mail-Ordner lassen sich nicht anlegen | `create_mailbox` | 2 · ✅ 20.11. in `main` (Cloud 18.11., auf v1.2.0 nachgezogen) |
| 3 | Keine Reminders (neue iCloud-Reminders vermutlich nicht über CalDAV erreichbar) | Abklären, ob machbar. Sonst bleiben Reminders beim Mac/Orchard. | 3 |
| 4 | `move_message` nur einzeln | Batch-Move mit Rückgabe der neuen UIDs | 2 · ✅ 20.11. in `main`: `move_messages` (1–50 UIDs, neue UIDs in Anfrage-Reihenfolge). Zusammen mit #2: 26 Tools bei voller Freigabe, `make check` grün. |
| 5 | Release-Binary aus fremder CI | Eigener Build aus dem Fork | 1 |
| 6 | Keine Entwürfe: `send_message` sendet direkt (Gegenstück zu Orchard #14, dort ✅ 27.11.) | `create_draft` per IMAP `APPEND` in den Drafts-Ordner mit `\Draft`-Flag, optional Anhänge (Pfadgrenze wie Orchard: nur unter Home, max. 10 Dateien / 25 MB; Antwortfelder wie Orchard: `messageId`, `draftMailbox`), Rückgabe von UID und Message-ID. Sendet nie. | 2 · offen |
| 7 | Mails lassen sich nicht exportieren (Gegenstück zu Orchard #15, dort ✅ 27.11.: `mail_export_message`, PDF mit gesperrten externen Inhalten) | `export_message` mit `format: eml` (Rohquelle per IMAP); PDF nur, wenn ohne Browser-Engine machbar, sonst bleibt PDF beim Mac/Orchard | 2 · offen |
| 8 | Breite Suche kann ins Zeitlimit laufen (Gegenstück zu Orchard #16, dort ✅ 27.11.: Zeitbudget, danach `partial: true` + `unsearchedMailboxes`) | Prüfen, ob `search_messages` über viele Ordner Teilergebnisse statt Abbruch liefern kann; Fehlermeldung mit Alternative | 2 · offen |
| 9 | iMessage lesen (Gegenstück zu Orchard Phase 6, dort ✅ 27.11. als `messages_*`, nur macOS) | Nicht machbar: iCloud bietet keine Schnittstelle für Nachrichten (nur verschlüsselt in «Nachrichten in iCloud»). Auf Linux deckt der geplante Matrix-Weg (Orchard Phase 6) die Messenger ab. | – · bewusst nicht umgesetzt |
| 10 | Kontakte lassen sich nicht in Gruppen ordnen, Nummern nicht vereinheitlichen (Gegenstück zu Orchard #20, dort ✅ 27.11.: Gruppen anlegen/umbenennen/löschen, Mitglieder, `normalize_phones` mit Vorschau) | Gruppen über CardDAV (Apple speichert sie als vCard mit `X-ADDRESSBOOKSERVER-KIND:group` und `X-ADDRESSBOOKSERVER-MEMBER`), dazu `normalize_phones` mit denselben Regeln (letzte 9 Ziffern, Vorschau zuerst) | 2 · offen. Linux-Orchard bräuchte das für dieselben Tools. |
| 11 | Kein Tool listet alle Kontakte mit Paging auf (Gegenstück zu Orchard #21, dort ✅ 27.11. als `contacts.list_contacts`). | Alle vCards des Adressbuchs über CardDAV holen, nach Name sortieren, `limit`/`offset`/`hasMore` wie bei Orchard. | offen |

## Phasen

| Phase | Inhalt | Status |
|---|---|---|
| 1 Setup | Fork ✅ 13.11. · upstream geprüft und gemergt ✅ 20.11. · Go installiert, gebaut, getestet ✅ 20.11. · Zugangsdaten anlegen, `--doctor`, Lesen testen · eigener Build (#5), Claude-Config auf eigenen Build umstellen | begonnen 20.11.2026 |
| 2 Mail | #1, #2, #4, #6–#8 | #2, #4 ✅ 20.11. in `main` · #1 wartet auf Review |
| 3 Abklären | #3 Reminders | noch nicht eingeplant |
| 4 Abschluss | Tests, README (kein Upstreaming, Entscheid 19.11.) | offen |
| 5 Anbindung Orchard | Zusammen mit Orchard Phase 5: Orchard startet icloud-mcp als Kindprozess (stdio), Tool-Zuordnung siehe `ROADMAP.md` im Orchard-Fork. Hier höchstens Kleinigkeiten nötig, z. B. stabile, maschinenlesbare Antworten. | Orchard-Seite ✅ 20.11.2026 (Orchard `main`, `src/icloud/`): startet icloud-mcp per `ICLOUD_MCP_BIN`, Smoke-Test gegen dieses Binary lokal ok. Offen: echter Test auf Linux, braucht Phase 1 (Zugangsdaten). |

Phase 1 ist Voraussetzung für Orchard Phase 5.

**27.11.2026:** Der Linux-Rechner soll die Dauerlösung werden (Claude-Code-Session mit Remote Control, unterwegs vom iPhone), siehe Orchard Phase 7. Damit werden Phase 1 und die Lücken #10, #11 (Kontakte schreiben/auflisten) und #3 (Reminders) wichtiger: Was icloud-mcp nicht kann, fehlt unterwegs.

## Branches (Stand 20.11.2026, alle auf `origin`)

| Branch | Inhalt | Status |
|---|---|---|
| `main` | upstream v1.2.0 + `create_mailbox` + `move_messages` | aktueller Stand, Binary per `go install` |
| `feat/mail-batch-v1.2` | dasselbe wie `main` (Arbeitsbranch) | erledigt |
| `feat/create-mailbox-v1.2`, `feat/move-messages-v1.2` | einzeln auf v1.2.0 | erledigt, in `main` |
| `feat/custom-mail-server-v1.2` | #1 auf v1.2.0 | wartet auf Review (Fork-PR #2) |
| `feat/setup-ui` | GUI für Mailkonten, Basis v1.1.1 | WIP, Guard-Entscheid offen |
| `feat/create-mailbox`, `feat/move-messages`, `feat/custom-mail-server` | Cloud-Stände auf v1.1.1 | überholt durch die `-v1.2`-Branches |
| `archive/cloud/claude/inspiring-cray-n2nrrt` | Cloud-Session 18.11. (Fork-PR #1, geschlossen) | Archiv |
| `sync/upstream-v1.2.0` | Upstream-Merge v1.2.0 | erledigt, in `main` |
