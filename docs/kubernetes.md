# Phoedox auf einem vServer mit k3s

Diese Anleitung richtet Phoedox auf **einem** Server als Single-Node-Kubernetes ein.

## Warum k3s

| | k3s | MicroK8s | k0s | Talos | kubeadm |
|---|---|---|---|---|---|
| Installation | ein Befehl, ein Binary | Snap | ein Binary | eigenes Betriebssystem | viele Schritte |
| Speicherbedarf (Leerlauf) | ca. 0,5 GB | ca. 0,8 GB | ca. 0,5 GB | ca. 0,5 GB | > 1 GB |
| Ingress dabei | Traefik | Add-on | nein | nein | nein |
| Storage dabei | local-path | Add-on | nein | nein | nein |
| Load-Balancer dabei | ServiceLB (Ports 80/443 am Host) | Add-on | nein | nein | nein |
| Updates | Installer erneut oder system-upgrade-controller | Snap-Kanal | `k0s` | API | manuell |

**Empfehlung: k3s.** Es ist für genau diesen Fall gebaut (wenige Ressourcen, ein Knoten), bringt
Ingress, Storage und Load-Balancer mit, ist CNCF-zertifiziert und läuft auf jedem üblichen
vServer-Image (Debian/Ubuntu). Talos ist sicherer, braucht aber ein eigenes OS-Image, das viele
vServer-Anbieter nicht anbieten. Der Weg zu mehreren Knoten bleibt offen (k3s-Agenten dazunehmen).

Dazu:

- **CloudNativePG** betreibt Postgres im Cluster: Datenbank, Nutzer (ohne Superuser-Rechte, damit
  Row-Level-Security greift) und auf Wunsch fortlaufende Sicherung nach S3.
- **cert-manager** holt Zertifikate von Let's Encrypt.
- **Phoedox** kommt als Helm-Chart aus diesem Repository (`deploy/helm/phoedox`).

## Größe des Servers

| Ausstattung | genügt für |
|---|---|
| 2 vCPU, 4 GB RAM, 40 GB SSD | Phoedox + Postgres, kleines Team |
| 4 vCPU, 8 GB RAM | zusätzlich ClamAV (ca. 1,5 GB) und OCR großer PDFs |

## 1. Server vorbereiten

```bash
apt-get update && apt-get -y upgrade
```

Firewall: nur 22 (SSH), 80 und 443 öffnen. Die Kubernetes-API (6443) nicht öffentlich freigeben.

```bash
ufw allow 22/tcp
```

```bash
ufw allow 80/tcp
```

```bash
ufw allow 443/tcp
```

```bash
ufw enable
```

## 2. k3s installieren

```bash
curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL=stable sh -s - --write-kubeconfig-mode 600 --secrets-encryption
```

`--secrets-encryption` verschlüsselt Kubernetes-Secrets (Master-Key, Passwörter) auf der Platte.

```bash
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
```

```bash
kubectl get nodes
```

Helm installieren (siehe helm.sh), dann die beiden Operatoren:

```bash
helm repo add jetstack https://charts.jetstack.io
```

```bash
helm repo add cnpg https://cloudnative-pg.github.io/charts
```

```bash
helm install cert-manager jetstack/cert-manager -n cert-manager --create-namespace --set crds.enabled=true
```

```bash
helm install cnpg cnpg/cloudnative-pg -n cnpg-system --create-namespace
```

## 3. DNS

Einen A-Eintrag (und AAAA) für die Domain, z. B. `docs.example.org`, auf die IP des Servers setzen.
Let's Encrypt prüft die Domain über Port 80.

## 4. Phoedox installieren

Eine Datei `phoedox.yaml` mit den eigenen Werten anlegen (alle Optionen: `deploy/helm/phoedox/values.yaml`):

```yaml
domain: docs.example.org
certManager:
  email: admin@example.org
# Solange das Image privat ist:
imagePullSecrets:
  - name: ghcr
# Optional: Mails für Einladungen und Passwort-Reset
mail:
  enabled: true
  host: smtp.example.org
  from: "Phoedox <docs@example.org>"
  user: docs@example.org
  existingSecret: phoedox-mail   # Schlüssel: password
```

```bash
kubectl create namespace phoedox
```

Solange das Repository privat ist, ein Pull-Secret für ghcr.io (Token mit `read:packages`):

```bash
kubectl -n phoedox create secret docker-registry ghcr --docker-server=ghcr.io --docker-username=DEIN_GITHUB_NAME --docker-password=DEIN_TOKEN
```

```bash
helm install phoedox ./deploy/helm/phoedox -n phoedox -f phoedox.yaml
```

```bash
kubectl -n phoedox get pods -w
```

Ersten Admin anlegen:

```bash
kubectl -n phoedox exec -it deploy/phoedox -- phx admin create-user --admin --email du@example.org --name "Dein Name"
```

## 5. Sichern – drei Dinge

1. **Master-Key** (verschlüsselt Geheimnisse, Webhook-Secrets, 2FA) einmal sicher ablegen:

   ```bash
   kubectl -n phoedox get secret phoedox-master-key -o jsonpath='{.data.master-key}' | base64 -d; echo
   ```

2. **Phoedox-Backups** (Datenbank + Dateien in einem portablen Format, auch für einen Umzug):
   standardmäßig täglich auf das Daten-Volume – das liegt auf derselben Platte. Besser in einen
   S3-Bucket (z. B. Hetzner Object Storage): `s3.enabled: true` und `backups.target: s3`.
   Herunterladen und wiederherstellen geht auch unter **Einstellungen → Backups**.
3. **Postgres fortlaufend** (Point-in-Time-Recovery) mit CloudNativePG: `database.cnpg.backup.enabled: true`
   und ein Secret mit `ACCESS_KEY_ID` und `ACCESS_SECRET_KEY` für den Bucket.

Daten-Volume, Datenbank und Master-Key bleiben bei `helm uninstall` erhalten (`helm.sh/resource-policy: keep`).

## Aktualisieren

Phoedox (neues Image ziehen, Datenbank-Migrationen laufen beim Start):

```bash
helm upgrade phoedox ./deploy/helm/phoedox -n phoedox -f phoedox.yaml
```

```bash
kubectl -n phoedox rollout restart deploy/phoedox
```

k3s selbst: den Installationsbefehl aus Schritt 2 erneut ausführen (oder den
system-upgrade-controller von Rancher einsetzen).

## Optionen

| Wert | Wirkung |
|---|---|
| `image.variant: full` (Standard) | PDF-Vorschauen, durchsuchbare PDFs, OCR; `""` für das minimale Image |
| `database.type: sqlite` | ohne Postgres – reicht für kleine Instanzen, Backups bleiben portabel |
| `clamav.enabled: true` | Virenscan aller Uploads (eigener Pod, ca. 1,5 GB RAM) |
| `metrics.enabled: true` | `/metrics` für Prometheus, Token im Secret `phoedox-metrics`; `metrics.serviceMonitor` für den Prometheus-Operator |
| `oidc.enabled: true` | Anmeldung per SSO (Authentik, Keycloak, Entra ID …) inklusive Gruppen |
| `s3.enabled: true` | Dateien im Bucket statt auf dem Volume |

## Grenzen

- Eine Phoedox-Instanz (`replicas: 1`): Die Live-Bearbeitung hält ihre Räume im Speicher. Für
  mehrere Instanzen kommt später die Verteilung über Postgres LISTEN/NOTIFY.
- `local-path` liegt auf der Platte des Knotens – deshalb Backups nach S3.
