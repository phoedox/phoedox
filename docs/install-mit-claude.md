# Installing Phoedox – procedure for Claude (Tabby MCP)

This file is a work instruction for Claude. Give it to your local Claude with the sentence
**"Install Phoedox on my server via Tabby following this guide."**

---

## To Claude: framework

You set up Phoedox on a single vServer: k3s (Kubernetes), cert-manager,
CloudNativePG (Postgres) and the Phoedox Helm chart. You work through the **Tabby MCP** in a
terminal session that is connected to the server via SSH.

Rules:

1. **One step at a time.** After each command, read the output and do the stated
   check. If a check fails, do not continue. Instead, use the section
   "If something goes wrong" and report to the user.
2. **Every step can be repeated.** First check whether it is already done ("Already done?"),
   then run only what is missing. This way the guide can simply start again after an interruption.
3. **Long commands** (installation, `helm install`, waiting for pods) take minutes: read the
   terminal output again at intervals until the prompt is back. Do not start anything twice.
4. **Never put secrets in the chat.** The user types GitHub tokens into the terminal themselves
   (`read -s`). You show the master key only in the terminal and ask the user to copy it from there
   into their password manager – you do not repeat it and do not store it anywhere.
5. **Do not delete anything** that you did not create yourself in this procedure. No `helm uninstall`,
   no deleting namespaces, PVCs or secrets without explicit consent.
6. Before step 1, ask the user for the values below, and summarize everything at the end.

### Ask the user beforehand

| Value | Example | Required |
|---|---|---|
| `DOMAIN` – address of Phoedox | `docs.example.org` | yes |
| `EMAIL` – for Let's Encrypt and the first admin | `du@example.org` | yes |
| `NAME` – name of the first admin | `Jan Beispiel` | yes |
| `GH_USER` – GitHub user name (only if repo/image is private) | `jan` | conditional |
| Is a Tabby session to the server already open via SSH? As root or with sudo? | | yes |
| Backups to S3? (endpoint, bucket – the user types the keys themselves) | Hetzner Object Storage | optional |

Minimum server size: 2 vCPU, 4 GB RAM, 40 GB SSD; Debian 12/13 or Ubuntu 22.04/24.04.

---

## Step 0 – Check the connection and basics

```bash
id -u; . /etc/os-release; echo "$PRETTY_NAME"; nproc; free -g | awk '/Mem/{print $2" GB RAM"}'; df -h / | tail -1
```

- `id -u` is `0` → running as root; otherwise prefix all following commands with `sudo` (running `sudo -i`
  once is easiest; then the rest applies unchanged).
- Less than 4 GB RAM or not Debian/Ubuntu → tell the user and ask whether to continue anyway.

Set variables for the session (insert the user's values):

```bash
export DOMAIN=docs.example.org EMAIL=du@example.org
```

## Step 1 – Check DNS

```bash
echo "Server: $(curl -4 -s https://ifconfig.me)"; echo "DNS:    $(getent ahostsv4 $DOMAIN | awk 'NR==1{print $1}')"
```

Check: both addresses are the same. Otherwise, ask the user to point an **A record** (and AAAA if needed) for
`$DOMAIN` to the server IP, and wait (check again). Without a correct DNS record,
the certificate fails later.

## Step 2 – Update the system, firewall

```bash
apt-get update && DEBIAN_FRONTEND=noninteractive apt-get -y upgrade && apt-get -y install curl git ufw ca-certificates
```

Firewall – **allow SSH first, then enable it**, otherwise the session locks itself out:

```bash
ufw allow 22/tcp && ufw allow 80/tcp && ufw allow 443/tcp
```

Allow the internal k3s networks (pods, services), otherwise ufw blocks traffic in the cluster
(recommended by the k3s docs; this does not make anything reachable from outside):

```bash
ufw allow from 10.42.0.0/16 to any && ufw allow from 10.43.0.0/16 to any
```

```bash
ufw --force enable && ufw status
```

Check: status `active`, rules for 22, 80, 443. If SSH runs on a different port
(`ss -tlnp | grep sshd`), allow that port instead of 22. The Kubernetes API (6443) stays closed.

## Step 3 – k3s

Already done?

```bash
command -v k3s && k3s --version
```

If not present:

```bash
curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL=stable sh -s - --write-kubeconfig-mode 600 --secrets-encryption
```

```bash
grep -q KUBECONFIG ~/.bashrc || echo 'export KUBECONFIG=/etc/rancher/k3s/k3s.yaml' >> ~/.bashrc; export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
```

Check (it can take 30–60 s until `Ready`):

```bash
kubectl get nodes
```

## Step 4 – Helm

```bash
command -v helm || curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
```

```bash
helm version --short
```

## Step 5 – cert-manager and CloudNativePG

```bash
helm repo add jetstack https://charts.jetstack.io; helm repo add cnpg https://cloudnative-pg.github.io/charts; helm repo update
```

Already installed?

```bash
helm list -A | grep -E 'cert-manager|cnpg'
```

Install what is missing:

```bash
helm install cert-manager jetstack/cert-manager -n cert-manager --create-namespace --set crds.enabled=true --wait
```

```bash
helm install cnpg cnpg/cloudnative-pg -n cnpg-system --create-namespace --wait
```

Check: all pods `Running`.

```bash
kubectl get pods -n cert-manager; kubectl get pods -n cnpg-system
```

## Step 6 – Clarify access to repo and image

```bash
for u in https://github.com/phoedox/phoedox https://ghcr.io/v2/phoedox/phoedox/tags/list; do echo "$u $(curl -s -o /dev/null -w '%{http_code}' $u)"; done
```

- Repo `200` → public. `404` → private: the user needs a GitHub token (classic) with
  `repo` and `read:packages`.
- ghcr `401` does **not** automatically mean private (ghcr often wants an anonymous token
  even for public images). What is certain: repo private ⇒ image private ⇒ pull secret needed.

If private: ask the user to enter the token **themselves in the terminal**:

```bash
read -rsp "GitHub token: " GH_TOKEN; echo
```

```bash
export GH_USER=github-name
```

## Step 7 – Get the repo

```bash
cd /root && { [ -d phoedox ] && git -C phoedox pull --ff-only; } || git clone https://github.com/phoedox/phoedox.git
```

For a private repo, use this instead (the token stays only in the shell variable):

```bash
cd /root && { [ -d phoedox ] && git -C phoedox pull --ff-only; } || git clone "https://$GH_USER:$GH_TOKEN@github.com/phoedox/phoedox.git" && git -C phoedox remote set-url origin https://github.com/phoedox/phoedox.git
```

Check:

```bash
ls /root/phoedox/deploy/helm/phoedox/Chart.yaml
```

## Step 8 – Namespace, pull secret, values

```bash
kubectl get namespace phoedox || kubectl create namespace phoedox
```

Only for a private image (overwrites an existing secret with the same name):

```bash
kubectl -n phoedox create secret docker-registry ghcr --docker-server=ghcr.io --docker-username="$GH_USER" --docker-password="$GH_TOKEN" --dry-run=client -o yaml | kubectl apply -f -
```

Write the values file (for a public image, leave out the two `imagePullSecrets` lines).
If `/root/phoedox.yaml` already exists, show it first and ask the user before you replace it.

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

Optional S3 backups: endpoint and bucket from the user; the user types the keys themselves:

```bash
read -rsp "S3 Access Key: " S3_AK; echo; read -rsp "S3 Secret Key: " S3_SK; echo
```

```bash
kubectl -n phoedox create secret generic phoedox-files-s3 --from-literal=access-key="$S3_AK" --from-literal=secret-key="$S3_SK" --dry-run=client -o yaml | kubectl apply -f -
```

Then append to `/root/phoedox.yaml` (endpoint without `https://`):

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

## Step 9 – Install Phoedox

```bash
helm status phoedox -n phoedox >/dev/null 2>&1 && echo "already installed" || echo "new"
```

New (`new`):

```bash
helm install phoedox /root/phoedox/deploy/helm/phoedox -n phoedox -f /root/phoedox.yaml
```

Already installed (`already installed`; values changed or update):

```bash
helm upgrade phoedox /root/phoedox/deploy/helm/phoedox -n phoedox -f /root/phoedox.yaml
```

Wait until the database and Phoedox are running (first installation: 2–5 minutes, the Postgres image is
being pulled). Check repeatedly; do not get stuck with `-w`:

```bash
kubectl -n phoedox get pods,cluster,certificate
```

Check: pod `phoedox-…` `1/1 Running`, cluster `Cluster in healthy state`, certificate
`READY True`. Then from outside:

```bash
curl -s https://$DOMAIN/healthz; echo; curl -s https://$DOMAIN/readyz; echo
```

Expected: `{"status":"ok",…}` twice.

## Step 10 – Create the first admin

```bash
kubectl -n phoedox exec -it deploy/phoedox -- phx admin create-user --admin --email "$EMAIL" --name "Name of the user"
```

The command asks for a password in the terminal (at least 10 characters) – the user types it
themselves, not you. Afterwards, the user should sign in at `https://$DOMAIN` and confirm.

## Step 11 – Back up the master key

The master key encrypts secrets, webhook secrets and 2FA. Without it, this data is worthless
in backups. **Show it only in the terminal**; the user copies it into their password manager:

```bash
kubectl -n phoedox get secret phoedox-master-key -o jsonpath='{.data.master-key}' | base64 -d; echo
```

Then ask the user whether they have saved it, and clear the display with `clear`. Do not
copy the value into the chat.

Finally, remove the token variables from the shell:

```bash
unset GH_TOKEN S3_AK S3_SK; history -c
```

## Step 12 – Wrap-up

Summarize for the user:

- Address `https://$DOMAIN`, admin created, certificate valid.
- Backups: daily, locally on the data volume **or** in S3 (depending on step 8). Local is on
  the same disk – recommend S3 if it was not set up.
- Master key saved (confirmed by the user).
- Next step: in Phoedox, under **Settings → API tokens & MCP**, create a token with
  write permission and give it, together with the URL, to the Claude session that is to
  transfer the documentation.

---

## Updates later

```bash
cd /root/phoedox && git pull --ff-only && helm upgrade phoedox ./deploy/helm/phoedox -n phoedox -f /root/phoedox.yaml && kubectl -n phoedox rollout restart deploy/phoedox
```

The chart pulls moving tags (`main`, `latest`) again on every restart (`pullPolicy` Always), so
a restart is enough as soon as the new image is built (GitHub Actions "docker" green).

An update does not make an automatic backup yet – back up first in the interface under
**Settings → Backups** or:

```bash
kubectl -n phoedox exec deploy/phoedox -- phx admin backup
```

## Changing the domain

Example: the instance ran under `phoedox.com` and should move to `app.phoedox.com`; `phoedox.com` (and
`www.`) redirect there for the time being. Tell the user beforehand: passkeys must be registered
again afterwards, and everyone must sign in again; password, TOTP, data and tokens stay.

1. DNS: A record (and AAAA) for the new domain to the server; check with step 1. The records
   for the redirecting hosts must also point to the server.
2. Backup (see "Updates later").
3. Update the repo (the redirect needs the chart from commit "chart: redirect hosts" on):

```bash
cd /root/phoedox && git pull --ff-only
```

4. In `/root/phoedox.yaml`, change `domain:` and add the redirect – show first, then
   change:

```bash
sed -i 's/^domain: .*/domain: app.phoedox.com/' /root/phoedox.yaml && grep -q redirectFrom /root/phoedox.yaml || printf 'ingress:\n  redirectFrom: [phoedox.com, www.phoedox.com]\n' >> /root/phoedox.yaml; cat /root/phoedox.yaml
```

   If the file already has an `ingress:` block, insert `redirectFrom` there instead of appending
   a second one. Only include `www.` if there is a DNS record for it.

5. Roll out and check:

```bash
helm upgrade phoedox /root/phoedox/deploy/helm/phoedox -n phoedox -f /root/phoedox.yaml && kubectl -n phoedox rollout restart deploy/phoedox
```

```bash
kubectl -n phoedox get ingress,certificate
```

```bash
curl -sI https://phoedox.com/projects | grep -i location; curl -s https://app.phoedox.com/readyz; echo
```

Expected: certificates `READY True` (one to two minutes), `location: https://app.phoedox.com/projects`
and `ready`. Afterwards, the user signs in at the new address; MCP clients and scripts
need the new URL.

## If something goes wrong

| Sign | Cause and solution |
|---|---|
| Pod `ImagePullBackOff` | Image is private and the pull secret is missing, or the token lacks `read:packages` → step 6/8. Details: `kubectl -n phoedox describe pod -l app.kubernetes.io/name=phoedox` |
| Pod `CrashLoopBackOff` | Read the logs: `kubectl -n phoedox logs deploy/phoedox --tail=50`. If it is waiting for the database, the Postgres cluster is not ready yet (`kubectl -n phoedox get cluster`). |
| Certificate stays `False` | DNS does not point to the server, or port 80 is closed. `kubectl -n phoedox describe certificate`, `kubectl get challenges -A`. After fixing it, wait a few minutes. |
| `curl https://…` certificate error | Certificate not issued yet (see above); until then, Traefik uses a self-signed one. |
| `phx admin create-user` reports "exists" | The user already exists – try to sign in; otherwise run `phx admin` without arguments for help (e.g. resetting the password). |
| Memory short (`kubectl top node`, OOMKilled) | Server too small or ClamAV enabled – ask the user whether to use a larger server or turn ClamAV off. |

For everything else: summarize the output, do not attempt anything destructive, ask the user. More
options (SSO, mail, virus scan, metrics) are in `docs/kubernetes.md`.
