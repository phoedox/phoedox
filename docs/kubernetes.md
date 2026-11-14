# Phoedox on a vServer with k3s

This guide sets up Phoedox on **one** server as single-node Kubernetes.
If Claude should do the installation through a terminal MCP (e.g. Tabby): [install-mit-claude.md](install-mit-claude.md).

## Why k3s

| | k3s | MicroK8s | k0s | Talos | kubeadm |
|---|---|---|---|---|---|
| Installation | one command, one binary | Snap | one binary | its own operating system | many steps |
| Memory use (idle) | approx. 0.5 GB | approx. 0.8 GB | approx. 0.5 GB | approx. 0.5 GB | > 1 GB |
| Ingress included | Traefik | add-on | no | no | no |
| Storage included | local-path | add-on | no | no | no |
| Load balancer included | ServiceLB (ports 80/443 on the host) | add-on | no | no | no |
| Updates | run the installer again or system-upgrade-controller | Snap channel | `k0s` | API | manual |

**Recommendation: k3s.** It is built for exactly this case (few resources, one node). It includes
ingress, storage and a load balancer, is CNCF-certified and runs on every common
vServer image (Debian/Ubuntu). Talos is more secure, but needs its own OS image, which many
vServer providers do not offer. The path to several nodes stays open (add k3s agents).

In addition:

- **CloudNativePG** runs Postgres in the cluster: database, user (without superuser rights, so that
  Row-Level Security applies) and, if wanted, continuous backup to S3.
- **cert-manager** gets certificates from Let's Encrypt.
- **Phoedox** comes as a Helm chart from this repository (`deploy/helm/phoedox`).

## Server size

| Resources | enough for |
|---|---|
| 2 vCPU, 4 GB RAM, 40 GB SSD | Phoedox + Postgres, small team |
| 4 vCPU, 8 GB RAM | additionally ClamAV (approx. 1.5 GB) and OCR of large PDFs |

## 1. Prepare the server

```bash
apt-get update && apt-get -y upgrade
```

Firewall: open only 22 (SSH), 80 and 443. Do not expose the Kubernetes API (6443) publicly.

```bash
ufw allow 22/tcp
```

```bash
ufw allow 80/tcp
```

```bash
ufw allow 443/tcp
```

Allow the internal k3s networks (pods and services; according to the k3s docs, this is needed with ufw):

```bash
ufw allow from 10.42.0.0/16 to any
```

```bash
ufw allow from 10.43.0.0/16 to any
```

```bash
ufw enable
```

## 2. Install k3s

```bash
curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL=stable sh -s - --write-kubeconfig-mode 600 --secrets-encryption
```

`--secrets-encryption` encrypts Kubernetes secrets (master key, passwords) on disk.

```bash
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
```

```bash
kubectl get nodes
```

Install Helm (see helm.sh), then the two operators:

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

Point an A record (and AAAA) for the domain, e.g. `docs.example.org`, to the IP of the server.
Let's Encrypt checks the domain over port 80.

## 4. Install Phoedox

Create a file `phoedox.yaml` with your own values (all options: `deploy/helm/phoedox/values.yaml`):

```yaml
domain: docs.example.org
certManager:
  email: admin@example.org
# While the image is private:
imagePullSecrets:
  - name: ghcr
# Optional: mail for invitations and password resets
mail:
  enabled: true
  host: smtp.example.org
  from: "Phoedox <docs@example.org>"
  user: docs@example.org
  existingSecret: phoedox-mail   # key: password
```

```bash
kubectl create namespace phoedox
```

As long as the repository is private, create a pull secret for ghcr.io (token with `read:packages`):

```bash
kubectl -n phoedox create secret docker-registry ghcr --docker-server=ghcr.io --docker-username=YOUR_GITHUB_NAME --docker-password=YOUR_TOKEN
```

```bash
helm install phoedox ./deploy/helm/phoedox -n phoedox -f phoedox.yaml
```

```bash
kubectl -n phoedox get pods -w
```

Create the first admin:

```bash
kubectl -n phoedox exec -it deploy/phoedox -- phx admin create-user --admin --email you@example.org --name "Your Name"
```

## 5. Back up – three things

1. Store the **master key** (encrypts secrets, webhook secrets, 2FA) safely once:

   ```bash
   kubectl -n phoedox get secret phoedox-master-key -o jsonpath='{.data.master-key}' | base64 -d; echo
   ```

2. **Phoedox backups** (database + files in a portable format, also for a move):
   by default daily to the data volume – which is on the same disk. Better to an
   S3 bucket (e.g. Hetzner Object Storage): `s3.enabled: true` and `backups.target: s3`.
   You can also download and restore under **Settings → Backups**.
3. **Postgres continuously** (point-in-time recovery) with CloudNativePG: `database.cnpg.backup.enabled: true`
   and a secret with `ACCESS_KEY_ID` and `ACCESS_SECRET_KEY` for the bucket.

The data volume, database and master key are kept on `helm uninstall` (`helm.sh/resource-policy: keep`).

## Updating

Phoedox (pull the new image; database migrations run on start):

```bash
helm upgrade phoedox ./deploy/helm/phoedox -n phoedox -f phoedox.yaml
```

```bash
kubectl -n phoedox rollout restart deploy/phoedox
```

k3s itself: run the installation command from step 2 again (or use the
system-upgrade-controller from Rancher).

## Options

| Value | Effect |
|---|---|
| `image.variant: full` (default) | PDF previews, searchable PDFs, OCR; `""` for the minimal image |
| `database.type: sqlite` | without Postgres – enough for small instances; backups stay portable |
| `clamav.enabled: true` | Virus scan of all uploads (own pod, approx. 1.5 GB RAM) |
| `metrics.enabled: true` | `/metrics` for Prometheus, token in the secret `phoedox-metrics`; `metrics.serviceMonitor` for the Prometheus Operator |
| `oidc.enabled: true` | Sign-in via SSO (Authentik, Keycloak, Entra ID …) including groups |
| `s3.enabled: true` | Files in the bucket instead of on the volume |
| `ingress.redirectFrom: [example.org]` | Additional hosts redirect permanently to `domain` (with certificate), e.g. the bare domain, as long as no website runs there |
| `ingress.httpsRedirect` (default `true`) | Permanently redirect `http://` to `https://` |

## Limits

- One Phoedox instance (`replicas: 1`): live editing keeps its rooms in memory. For
  several instances, distribution via Postgres LISTEN/NOTIFY will come later.
- `local-path` is on the node's disk – that is why backups go to S3.

## Publications under your own domain

Publications (Settings → Publications) are reachable under `https://<app-domain>/pub/<short-name>/`.
For your own domain such as `docs.phoedox.com`:

1. Point a DNS record (A/AAAA or CNAME) to the server.
2. Add this to the chart and run `helm upgrade` – the domain gets an ingress rule and a certificate:

   ```yaml
   publications:
     domains: [docs.phoedox.com]
   ```

3. In Phoedox, enter the same domain for the publication.
