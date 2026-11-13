# Phoedox installieren – Ablauf für Claude (Tabby MCP)

Diese Datei ist eine Arbeitsanweisung für Claude. Gib sie deinem lokalen Claude mit dem Satz
**„Installiere Phoedox nach dieser Anleitung über Tabby auf meinem Server.“**

---

## An Claude: Rahmen

Du richtest Phoedox auf einem einzelnen vServer ein: k3s (Kubernetes), cert-manager,
CloudNativePG (Postgres) und das Phoedox-Helm-Chart. Du arbeitest über das **Tabby-MCP** in einer
Terminal-Sitzung, die per SSH mit dem Server verbunden ist.

Regeln:

1. **Einen Schritt nach dem anderen.** Nach jedem Befehl die Ausgabe lesen und die angegebene
   Prüfung machen. Geht eine Prüfung schief, nicht weitermachen, sondern den Abschnitt
   „Wenn etwas schiefgeht“ nutzen und dem Nutzer berichten.
2. **Jeder Schritt ist wiederholbar.** Erst prüfen, ob er schon erledigt ist („Schon erledigt?“),
   dann nur ausführen, was fehlt. So kann die Anleitung nach einem Abbruch einfach neu starten.
3. **Lange Befehle** (Installation, `helm install`, Warten auf Pods) brauchen Minuten: die
   Terminal-Ausgabe in Abständen erneut lesen, bis der Prompt zurück ist. Nichts doppelt starten.
4. **Geheimnisse nie in den Chat.** GitHub-Token tippt der Nutzer selbst im Terminal ein
   (`read -s`). Den Master-Key zeigst du nur im Terminal an und bittest den Nutzer, ihn von dort
   in seinen Passwortmanager zu kopieren – du gibst ihn nicht wieder und speicherst ihn nirgends.
5. **Nichts löschen**, was du nicht selbst in diesem Ablauf angelegt hast. Kein `helm uninstall`,
   kein Löschen von Namespaces, PVCs oder Secrets ohne ausdrückliche Zustimmung.
6. Vor Schritt 1 die Werte unten beim Nutzer erfragen und am Ende alles zusammenfassen.

### Vorher vom Nutzer erfragen

| Wert | Beispiel | Pflicht |
|---|---|---|
| `DOMAIN` – Adresse von Phoedox | `docs.example.org` | ja |
| `EMAIL` – für Let's Encrypt und den ersten Admin | `du@example.org` | ja |
| `NAME` – Name des ersten Admins | `Jan Beispiel` | ja |
| `GH_USER` – GitHub-Benutzername (nur falls Repo/Image privat) | `jan` | bedingt |
| Ist schon eine Tabby-Sitzung per SSH zum Server offen? Als root oder mit sudo? | | ja |
| Backups nach S3? (Endpoint, Bucket – Schlüssel tippt der Nutzer selbst) | Hetzner Object Storage | optional |

Server-Mindestgröße: 2 vCPU, 4 GB RAM, 40 GB SSD; Debian 12/13 oder Ubuntu 22.04/24.04.

---

## Schritt 0 – Verbindung und Grundlagen prüfen

```bash
id -u; . /etc/os-release; echo "$PRETTY_NAME"; nproc; free -g | awk '/Mem/{print $2" GB RAM"}'; df -h / | tail -1
```

- `id -u` ist `0` → als root, sonst allen folgenden Befehlen `sudo` voranstellen (`sudo -i` einmal
  ausführen ist am einfachsten, dann gilt der Rest unverändert).
- Weniger als 4 GB RAM oder kein Debian/Ubuntu → dem Nutzer melden und fragen, ob trotzdem.

Variablen für die Sitzung setzen (Werte des Nutzers einsetzen):

```bash
export DOMAIN=docs.example.org EMAIL=du@example.org
```

## Schritt 1 – DNS prüfen

```bash
echo "Server: $(curl -4 -s https://ifconfig.me)"; echo "DNS:    $(getent ahostsv4 $DOMAIN | awk 'NR==1{print $1}')"
```

Prüfung: beide Adressen gleich. Sonst den Nutzer bitten, einen **A-Eintrag** (und ggf. AAAA) für
`$DOMAIN` auf die Server-IP zu setzen, und warten (erneut prüfen). Ohne korrekten DNS-Eintrag
scheitert später das Zertifikat.

## Schritt 2 – System aktualisieren, Firewall

```bash
apt-get update && DEBIAN_FRONTEND=noninteractive apt-get -y upgrade && apt-get -y install curl git ufw ca-certificates
```

Firewall – **erst SSH erlauben, dann einschalten**, sonst sperrt sich die Sitzung aus:

```bash
ufw allow 22/tcp && ufw allow 80/tcp && ufw allow 443/tcp
```

Die internen k3s-Netze (Pods, Services) zulassen, sonst blockiert ufw den Verkehr im Cluster
(Empfehlung der k3s-Doku; von außen wird dadurch nichts erreichbar):

```bash
ufw allow from 10.42.0.0/16 to any && ufw allow from 10.43.0.0/16 to any
```

```bash
ufw --force enable && ufw status
```

Prüfung: Status `active`, Regeln für 22, 80, 443. Läuft SSH auf einem anderen Port
(`ss -tlnp | grep sshd`), diesen statt 22 freigeben. Die Kubernetes-API (6443) bleibt zu.

## Schritt 3 – k3s

Schon erledigt?

```bash
command -v k3s && k3s --version
```

Falls nicht vorhanden:

```bash
curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL=stable sh -s - --write-kubeconfig-mode 600 --secrets-encryption
```

```bash
grep -q KUBECONFIG ~/.bashrc || echo 'export KUBECONFIG=/etc/rancher/k3s/k3s.yaml' >> ~/.bashrc; export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
```

Prüfung (kann 30–60 s dauern, bis `Ready`):

```bash
kubectl get nodes
```

## Schritt 4 – Helm

```bash
command -v helm || curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
```

```bash
helm version --short
```

## Schritt 5 – cert-manager und CloudNativePG

```bash
helm repo add jetstack https://charts.jetstack.io; helm repo add cnpg https://cloudnative-pg.github.io/charts; helm repo update
```

Schon installiert?

```bash
helm list -A | grep -E 'cert-manager|cnpg'
```

Fehlendes installieren:

```bash
helm install cert-manager jetstack/cert-manager -n cert-manager --create-namespace --set crds.enabled=true --wait
```

```bash
helm install cnpg cnpg/cloudnative-pg -n cnpg-system --create-namespace --wait
```

Prüfung: alle Pods `Running`.

```bash
kubectl get pods -n cert-manager; kubectl get pods -n cnpg-system
```

## Schritt 6 – Zugriff auf Repo und Image klären

```bash
for u in https://github.com/phoedox/phoedox https://ghcr.io/v2/phoedox/phoedox/tags/list; do echo "$u $(curl -s -o /dev/null -w '%{http_code}' $u)"; done
```

- Repo `200` → öffentlich. `404` → privat: Der Nutzer braucht einen GitHub-Token (classic) mit
  `repo` und `read:packages`.
- ghcr `401` heißt **nicht** automatisch privat (ghcr will auch für öffentliche Images oft ein
  anonymes Token). Sicher ist: Repo privat ⇒ Image privat ⇒ Pull-Secret nötig.

Falls privat: den Nutzer bitten, den Token **selbst im Terminal** einzugeben:

```bash
read -rsp "GitHub-Token: " GH_TOKEN; echo
```

```bash
export GH_USER=github-name
```

## Schritt 7 – Repo holen

```bash
cd /root && { [ -d phoedox ] && git -C phoedox pull --ff-only; } || git clone https://github.com/phoedox/phoedox.git
```

Bei privatem Repo stattdessen (der Token bleibt nur in der Shell-Variable):

```bash
cd /root && { [ -d phoedox ] && git -C phoedox pull --ff-only; } || git clone "https://$GH_USER:$GH_TOKEN@github.com/phoedox/phoedox.git" && git -C phoedox remote set-url origin https://github.com/phoedox/phoedox.git
```

Prüfung:

```bash
ls /root/phoedox/deploy/helm/phoedox/Chart.yaml
```

## Schritt 8 – Namespace, Pull-Secret, Werte

```bash
kubectl get namespace phoedox || kubectl create namespace phoedox
```

Nur bei privatem Image (überschreibt ein vorhandenes Secret gleichen Namens):

```bash
kubectl -n phoedox create secret docker-registry ghcr --docker-server=ghcr.io --docker-username="$GH_USER" --docker-password="$GH_TOKEN" --dry-run=client -o yaml | kubectl apply -f -
```

Werte-Datei schreiben (bei öffentlichem Image die beiden `imagePullSecrets`-Zeilen weglassen).
Gibt es `/root/phoedox.yaml` schon, erst zeigen und den Nutzer fragen, bevor du sie ersetzt.

```bash
cat > /root/phoedox.yaml <<EOF
domain: $DOMAIN
certManager:
  email: $EMAIL
imagePullSecrets:
  - name: ghcr
env:
  # The public plugin store is not online yet.
  PHOEDOX_STORE_URL: "off"
EOF
```

```bash
cat /root/phoedox.yaml
```

Optional S3-Backups: Endpoint und Bucket vom Nutzer, Schlüssel tippt er selbst:

```bash
read -rsp "S3 Access Key: " S3_AK; echo; read -rsp "S3 Secret Key: " S3_SK; echo
```

```bash
kubectl -n phoedox create secret generic phoedox-files-s3 --from-literal=access-key="$S3_AK" --from-literal=secret-key="$S3_SK" --dry-run=client -o yaml | kubectl apply -f -
```

Dann an `/root/phoedox.yaml` anhängen (Endpoint ohne `https://`):

```bash
cat >> /root/phoedox.yaml <<EOF
s3:
  enabled: true
  endpoint: fsn1.your-objectstorage.com
  bucket: phoedox
backups:
  target: s3
EOF
```

## Schritt 9 – Phoedox installieren

```bash
helm status phoedox -n phoedox >/dev/null 2>&1 && echo "schon installiert" || echo "neu"
```

Neu:

```bash
helm install phoedox /root/phoedox/deploy/helm/phoedox -n phoedox -f /root/phoedox.yaml
```

Schon installiert (Werte geändert oder Update):

```bash
helm upgrade phoedox /root/phoedox/deploy/helm/phoedox -n phoedox -f /root/phoedox.yaml
```

Warten, bis Datenbank und Phoedox laufen (erste Installation: 2–5 Minuten, Postgres-Image wird
geladen). Wiederholt prüfen, nicht mit `-w` hängen bleiben:

```bash
kubectl -n phoedox get pods,cluster,certificate
```

Prüfung: Pod `phoedox-…` `1/1 Running`, Cluster `Cluster in healthy state`, Certificate
`READY True`. Dann von außen:

```bash
curl -s https://$DOMAIN/healthz; echo; curl -s https://$DOMAIN/readyz; echo
```

Erwartet: `{"status":"ok",…}` zweimal.

## Schritt 10 – Ersten Admin anlegen

```bash
kubectl -n phoedox exec -it deploy/phoedox -- phx admin create-user --admin --email "$EMAIL" --name "Name des Nutzers"
```

Der Befehl fragt im Terminal nach einem Passwort (mindestens 10 Zeichen) – der Nutzer tippt es
selbst ein, nicht du. Danach soll der Nutzer sich unter `https://$DOMAIN` anmelden und bestätigen.

## Schritt 11 – Master-Key sichern

Der Master-Key verschlüsselt Geheimnisse, Webhook-Secrets und 2FA. Ohne ihn sind diese Daten in
Backups wertlos. **Nur im Terminal anzeigen**, der Nutzer kopiert ihn in seinen Passwortmanager:

```bash
kubectl -n phoedox get secret phoedox-master-key -o jsonpath='{.data.master-key}' | base64 -d; echo
```

Danach den Nutzer fragen, ob er ihn gesichert hat, und mit `clear` die Anzeige leeren. Den Wert
nicht in den Chat übernehmen.

Zum Schluss die Token-Variablen aus der Shell entfernen:

```bash
unset GH_TOKEN S3_AK S3_SK; history -c
```

## Schritt 12 – Abschluss

Dem Nutzer zusammenfassen:

- Adresse `https://$DOMAIN`, Admin angelegt, Zertifikat gültig.
- Backups: täglich, lokal auf dem Daten-Volume **oder** in S3 (je nach Schritt 8). Lokal liegt auf
  derselben Platte – S3 empfehlen, falls nicht gemacht.
- Master-Key gesichert (vom Nutzer bestätigt).
- Nächster Schritt: In Phoedox unter **Einstellungen → API-Tokens & MCP** einen Token mit
  Schreibrecht anlegen und zusammen mit der URL an die Claude-Sitzung geben, die die Doku
  übertragen soll.

---

## Updates später

```bash
cd /root/phoedox && git pull --ff-only && helm upgrade phoedox ./deploy/helm/phoedox -n phoedox -f /root/phoedox.yaml && kubectl -n phoedox rollout restart deploy/phoedox
```

Das Chart lädt bewegliche Tags (`main`, `latest`) bei jedem Neustart neu (`pullPolicy` Always), daher
genügt der Neustart, sobald das neue Image gebaut ist (GitHub Actions „docker“ grün).

Ein Update macht noch kein automatisches Backup – vorher in der Oberfläche unter
**Einstellungen → Backups** sichern oder:

```bash
kubectl -n phoedox exec deploy/phoedox -- phx admin backup
```

## Domain wechseln

Beispiel: Die Instanz lief unter `phoedox.com` und soll nach `app.phoedox.com`; `phoedox.com` (und
`www.`) leiten so lange dorthin weiter. Vorher dem Nutzer sagen: Passkeys müssen danach neu
registriert werden, alle müssen sich neu anmelden; Passwort, TOTP, Daten und Tokens bleiben.

1. DNS: A-Eintrag (und AAAA) für die neue Domain auf den Server; mit Schritt 1 prüfen. Für die
   weiterleitenden Hosts müssen die Einträge ebenfalls auf den Server zeigen.
2. Backup (siehe „Updates später“).
3. Repo aktualisieren (die Weiterleitung braucht das Chart ab Commit „chart: redirect hosts“):

```bash
cd /root/phoedox && git pull --ff-only
```

4. In `/root/phoedox.yaml` `domain:` ändern und die Weiterleitung ergänzen – erst zeigen, dann
   ändern:

```bash
sed -i 's/^domain: .*/domain: app.phoedox.com/' /root/phoedox.yaml && grep -q redirectFrom /root/phoedox.yaml || printf 'ingress:\n  redirectFrom: [phoedox.com, www.phoedox.com]\n' >> /root/phoedox.yaml; cat /root/phoedox.yaml
```

   Gibt es in der Datei schon einen `ingress:`-Block, `redirectFrom` dort einfügen statt einen
   zweiten anzuhängen. `www.` nur aufnehmen, wenn es dafür einen DNS-Eintrag gibt.

5. Ausrollen und prüfen:

```bash
helm upgrade phoedox /root/phoedox/deploy/helm/phoedox -n phoedox -f /root/phoedox.yaml && kubectl -n phoedox rollout restart deploy/phoedox
```

```bash
kubectl -n phoedox get ingress,certificate
```

```bash
curl -sI https://phoedox.com/projects | grep -i location; curl -s https://app.phoedox.com/readyz; echo
```

Erwartet: Zertifikate `READY True` (ein bis zwei Minuten), `location: https://app.phoedox.com/projects`
und `ready`. Danach meldet sich der Nutzer unter der neuen Adresse an; MCP-Clients und Skripte
brauchen die neue URL.

## Wenn etwas schiefgeht

| Zeichen | Ursache und Lösung |
|---|---|
| Pod `ImagePullBackOff` | Image privat und Pull-Secret fehlt oder Token ohne `read:packages` → Schritt 6/8. Details: `kubectl -n phoedox describe pod -l app.kubernetes.io/name=phoedox` |
| Pod `CrashLoopBackOff` | Logs lesen: `kubectl -n phoedox logs deploy/phoedox --tail=50`. Wartet er auf die Datenbank, ist der Postgres-Cluster noch nicht bereit (`kubectl -n phoedox get cluster`). |
| Certificate bleibt `False` | DNS zeigt nicht auf den Server oder Port 80 ist zu. `kubectl -n phoedox describe certificate`, `kubectl get challenges -A`. Nach Korrektur einige Minuten warten. |
| `curl https://…` Zertifikatsfehler | Zertifikat noch nicht ausgestellt (siehe oben); Traefik nutzt bis dahin ein Selbstsigniertes. |
| `phx admin create-user` meldet „exists“ | Nutzer gibt es schon – Anmeldung versuchen, sonst `phx admin` ohne Argumente für Hilfe (z. B. Passwort zurücksetzen). |
| Speicher knapp (`kubectl top node`, OOMKilled) | Server zu klein oder ClamAV aktiv – Nutzer fragen, ob größerer Server oder ClamAV aus. |

Bei allem anderen: Ausgabe zusammenfassen, keine Zerstörung versuchen, Nutzer fragen. Weitere
Optionen (SSO, Mail, Virenscan, Metriken) stehen in `docs/kubernetes.md`.
