# Putting APEX Billing on the Linux server

For a Linux server (Ubuntu / Debian) in the office network, opened by other
PCs at **https://SERVER/**. The code comes from GitHub (`git clone`), the
server builds it, the program serves HTTPS itself, the database is on the same
server, and it runs as a locked-down service.

In the steps, replace:
- `SERVER` with the server's address (e.g. `192.168.1.50`)
- `USER` with your login name on the server (e.g. `ubuntu`)
- `GITHUB-NAME` with your GitHub user name

Commands marked **(PC)** go in a Command Prompt on your Windows PC, in the
apex-billing folder. Commands marked **(server)** go in the SSH window.
Passwords are typed only when asked — **never into a file in the repository**.

---

## 1. Put the code on GitHub (PC)

1. Check Git is installed: `git --version`. If not, install it from
   https://git-scm.com/download/win (default options), then open a new
   Command Prompt.
2. On https://github.com: **New repository** → name `apex-billing` → the
   visibility your team chose → **don't** tick README, .gitignore or licence →
   **Create repository**.
3. Your commits show an email address publicly. Use GitHub's private one
   (GitHub → Settings → Emails → "Keep my email addresses private" shows it,
   like `12345+name@users.noreply.github.com`):
   ```
   git config --global user.name "Your Name"
   git config --global user.email "12345+name@users.noreply.github.com"
   ```
4. In the apex-billing folder:
   ```
   git init -b main
   git add .
   git status
   ```
   **Read the list before going on.** It must NOT contain `.env`, `.env.old`,
   `apex-billing.exe`, anything in `uploads/` or `tls/`, or any `.pem` file.
   (The `.gitignore` file keeps them out; if one shows, stop and ask.)
5. Upload:
   ```
   git commit -m "APEX Billing"
   git remote add origin https://github.com/GITHUB-NAME/apex-billing.git
   git push -u origin main
   ```
   A GitHub sign-in window opens the first time.

Later changes: `git add .` → `git status` (check again) →
`git commit -m "what changed"` → `git push`.

## 2. Make the HTTPS certificate (PC)

Browsers only trust the server if its certificate comes from an authority
they trust. **mkcert** makes a small private authority for your office.

1. Download `mkcert-v1.4.4-windows-amd64.exe` from
   https://github.com/FiloSottile/mkcert/releases, save it in the
   apex-billing folder as `mkcert.exe` (Git ignores it).
2. Run (the server's address, and its name too if it has one):
   ```
   mkcert -install
   mkdir tls
   mkcert -cert-file tls\cert.pem -key-file tls\key.pem SERVER
   mkcert -CAROOT
   ```
   The last command shows a folder. In it, **`rootCA.pem`** is what other PCs
   need. **`rootCA-key.pem` must never leave your PC** and never goes to
   GitHub (anyone with it can make certificates your office trusts).
3. Copy the certificate to the server (it is not in the repository):
   ```
   scp tls\cert.pem tls\key.pem USER@SERVER:~/
   ```
   The first time it asks "Are you sure you want to continue connecting?":
   type `yes`.

## 3. Connect to the server (PC)

```
ssh USER@SERVER
```

Everything below is typed in this window **(server)**. If your colleague sent
you this password in a message, change it now: `passwd`.

## 4. Tools for building (server)

```
sudo apt update && sudo apt upgrade -y
sudo apt install -y git curl ufw unattended-upgrades
uname -m
go version
node -v
```

- `uname -m`: `x86_64` → use `amd64` below; `aarch64` → use `arm64`.
- **Go** must be 1.26 or newer. If `go version` fails or is older:
  ```
  curl -LO https://go.dev/dl/go1.26.3.linux-amd64.tar.gz
  sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.26.3.linux-amd64.tar.gz
  echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile && . ~/.profile
  rm go1.26.3.linux-amd64.tar.gz
  go version
  ```
- **Node.js** must be 20 or newer. If `node -v` fails or is older:
  ```
  curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash -
  sudo apt install -y nodejs
  node -v
  ```

## 5. Get the code and build it (server)

```
cd ~
git clone https://github.com/GITHUB-NAME/apex-billing.git
cd apex-billing
chmod +x build.sh
./build.sh
```

It ends with `Done: ./apex-billing`.

## 6. Create the database and its limited user (server)

PostgreSQL is already installed. Make a strong password for the app's
database user and **write it down** (you need it in step 8):

```
openssl rand -hex 24
```

Put it into a private copy of the setup script, run it, delete the copy:

```
umask 077
cp scripts/db-setup.sql ~/db-setup.sql
nano ~/db-setup.sql
```
Replace `CHANGE-THIS-to-a-long-random-password` with that password, save
(Ctrl+O, Enter, Ctrl+X). Then:

```
sudo -u postgres psql < ~/db-setup.sql
shred -u ~/db-setup.sql
umask 022
```

(If the `apex_billing_app` user already existed, the script keeps its old
password; set the new one with
`sudo -u postgres psql -c "ALTER ROLE apex_billing_app PASSWORD 'the-password'"`.)

Keep PostgreSQL listening only on the server itself (in
`/etc/postgresql/*/main/postgresql.conf`: `listen_addresses = 'localhost'`).

## 7. Install the program (server)

```
sudo useradd --system --home /nonexistent --shell /usr/sbin/nologin apexbilling
sudo mkdir -p /opt/apex-billing/tls /var/lib/apex-billing/uploads
sudo install -m 755 -o root -g root ~/apex-billing/apex-billing /opt/apex-billing/apex-billing
sudo install -m 640 -o root -g apexbilling ~/cert.pem ~/key.pem /opt/apex-billing/tls/
shred -u ~/key.pem && rm ~/cert.pem
sudo chown -R apexbilling:apexbilling /var/lib/apex-billing
sudo chmod 750 /var/lib/apex-billing
```

## 8. The settings file (server)

Make the secret key and **save a copy somewhere safe** (password manager):

```
openssl rand -hex 32
```

```
sudo install -m 640 -o root -g apexbilling ~/apex-billing/deploy/env.server.example /opt/apex-billing/.env
sudo nano /opt/apex-billing/.env
```

Fill in: `APP_SECRET` (the key above), `DB_PASSWORD` (from step 6),
`BILLING_ADMIN_EMAIL` and a temporary `BILLING_ADMIN_PASSWORD`. Save.
This file lives only on the server — it is not in the repository.

## 9. Start it as a service (server)

```
sudo cp ~/apex-billing/deploy/apex-billing.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now apex-billing
sudo systemctl status apex-billing
journalctl -u apex-billing -n 30 --no-pager
```

You should see `First admin created` and `starting on :443`, and **no**
"WARNING" lines about HTTPS or the postgres user.

## 10. Firewall (server)

Allow SSH first (or you lock yourself out), then HTTPS only from the office
network. Use your network's range — if the server is `192.168.1.50`, it is
`192.168.1.0/24`:

```
sudo ufw allow OpenSSH
sudo ufw allow from 192.168.1.0/24 to any port 443 proto tcp
sudo ufw enable
sudo ufw status
```

## 11. First sign-in, then remove the admin password (browser + server)

1. On your PC open **https://SERVER/** — sign in with the admin email and
   temporary password, choose a new password.
2. **My account → Two-step sign-in → Set up.** Then **Company settings →
   Security → Require two-step sign-in for everyone.**
3. Fill in the companies (logos), products, clients, services.
4. Remove the temporary password from the server:
   ```
   sudo nano /opt/apex-billing/.env
   ```
   delete the `BILLING_ADMIN_EMAIL` and `BILLING_ADMIN_PASSWORD` lines, save, then
   `sudo systemctl restart apex-billing`.

## 12. Daily backups (server)

```
sudo install -m 750 ~/apex-billing/scripts/backup.sh /usr/local/sbin/apex-billing-backup
sudo /usr/local/sbin/apex-billing-backup
echo '30 22 * * * root /usr/local/sbin/apex-billing-backup' | sudo tee /etc/cron.d/apex-billing-backup
```

Backups go to `/var/backups/apex-billing` (newest 30 kept, root only). Ask
your colleague to copy that folder off the server regularly (another machine
or an encrypted USB disk): a backup on the same disk doesn't survive a disk
failure.

**Test a restore** once now and then monthly:
```
sudo -u postgres createdb apex_billing_test
sudo -u postgres pg_restore -d apex_billing_test /var/backups/apex-billing/<date>/apex_billing.dump
sudo -u postgres dropdb apex_billing_test
```

## 13. Other PCs

On each PC that will use APEX Billing, install `rootCA.pem` (from step 2,
**not** the key) once. Copy it to the PC, then in a Command Prompt
**run as administrator**:

```
certutil -addstore -f Root rootCA.pem
```

Then open **https://SERVER/** — no warning should appear. (Firefox: Settings →
Privacy & Security → Certificates → View Certificates → Authorities → Import.)

---

## Updating to a new version later

PC: commit and `git push` the changes (step 1, "Later changes").

Server:
```
cd ~/apex-billing
git pull
./build.sh
sudo /usr/local/sbin/apex-billing-backup
sudo install -m 755 -o root -g root apex-billing /opt/apex-billing/apex-billing
sudo systemctl restart apex-billing
journalctl -u apex-billing -n 20 --no-pager
```

## If something goes wrong

- `journalctl -u apex-billing -n 50 --no-pager` shows why it stopped.
- "APP_SECRET is missing or weak": the key in `.env` is shorter than 32
  characters or still the placeholder.
- "Could not connect to the database": check `DB_PASSWORD` matches step 6.
- `./build.sh` says Go is too old or `go.mod requires go >= 1.26`: step 4.
- Browser warning about the certificate: that PC doesn't have `rootCA.pem`
  installed (step 13), or you opened the server by a name/address that wasn't
  in the `mkcert` command (step 2 — make a new certificate including it).
- Locked out of the admin account: on the server add `BILLING_ADMIN_EMAIL` /
  `BILLING_ADMIN_PASSWORD` to `.env`, run
  `sudo -u apexbilling sh -c 'cd /opt/apex-billing && ./apex-billing --reset-admin'`,
  then remove the lines again.
- **A password or key was pushed to GitHub by mistake:** change that password
  / key at once (deleting the file from GitHub is not enough — it stays in the
  history and copies may already exist).
