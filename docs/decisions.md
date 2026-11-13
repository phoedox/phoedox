# Entscheidungen

**Seit 13.11.2026 werden alle Produkt- und Technik-Entscheidungen in Phoedox selbst geführt:**
<https://app.phoedox.com/p/phoedox/decisions> (Projekt `phoedox`, Kategorie „Entscheidungen“).
Öffentlich lesbar werden sie mit der Veröffentlichung unter `docs.phoedox.com`.

Diese Datei wird nicht mehr gepflegt. Ihr letzter Stand liegt im Git-Verlauf (Commit vor dieser
Änderung); begründet in ADR-0026.

Neue Entscheidungen anlegen: in der Oberfläche oder per MCP mit `create_decision`
(bei Ersatz einer älteren `supersedes` setzen); lesen mit `list_decisions` und `get_document`.

## Übertragene Entscheidungen

| Nr. | Titel | früherer Abschnitt hier |
|---|---|---|
| ADR-0001 | Wissensnetz als Kern | Produkt, Doku-Typen |
| ADR-0002 | Technik-Stack | Technik |
| ADR-0003 | Plugins und Store: Grundsätze | Plugins & Store |
| ADR-0004 | Agent für Server, VMs und LXC | Agent, Runde 5 (Agent-Protokoll) |
| ADR-0005 | Home Assistant anbinden | Home Assistant |
| ADR-0006 | Veröffentlichungen und eigene Domains | Veröffentlichungen & Domains, Runde 5 (Veröffentlichungen) |
| ADR-0007 | Architektur: Abfragen, Echtzeit, Rechte, Jobs, Verteilung | Runde 5 |
| ADR-0008 | Betrieb: Image, Proxy, Cookies | Betrieb (erste Version) |
| ADR-0009 | Nummern und Ersetzen bei Entscheidungen | Entscheidungen (ADRs) |
| ADR-0010 | Konten, Einladungen und Passwörter | Konten |
| ADR-0011 | Zwei-Faktor und Passkeys | Zwei-Faktor & Passkeys |
| ADR-0012 | SSO und OAuth für MCP | SSO & OAuth für MCP |
| ADR-0013 | Suche in der Datenbank | Suche |
| ADR-0014 | Verlauf, Papierkorb und Favoriten | Verlauf, Papierkorb, Favoriten |
| ADR-0015 | Live-Bearbeitung mit Yjs und Lexical | Live-Bearbeitung |
| ADR-0016 | Request-Blöcke und Projekt-Umgebung | Request-Blöcke & Umgebung |
| ADR-0017 | KI-Zugriff fein steuern | KI-Zugriff |
| ADR-0018 | Datenmodell und Wissensnetz | Datenmodell & Wissensnetz |
| ADR-0019 | Dateien: Speicher, Uploads, Vorschauen | Dateien |
| ADR-0020 | Organisationen, Gruppen und Freigaben | Organisationen & Freigaben |
| ADR-0021 | Mandanten mit Row-Level-Security | Mandanten (gehostet) |
| ADR-0022 | Kubernetes auf einem Server mit k3s | Betrieb: Kubernetes |
| ADR-0023 | Betrieb: Jobs, Backups, Metriken, API, Webhooks | Betrieb: Jobs, Backups, … |
| ADR-0024 | Plugin-Laufzeit, Pakete und Store | Plugins: Laufzeit, Pakete, Store |
| ADR-0025 | Domains von phoedox.com | Domains von phoedox.com |
| ADR-0026 | Phoedox als führende Quelle für Entscheidungen | – |

Phasen, Feature-Listen der Runden 4 und 5 und der Stand der Umsetzung stehen auf der Seite
„Fahrplan und Stand“ im selben Projekt.
