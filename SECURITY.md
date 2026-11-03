# APEX Billing — security

APEX Billing holds client, invoice and payment data. This file says what
the program does to protect it, and what **you** must set up when you
install it. Work through the checklist once; then the routine at the end.

## What the program does

- **Sign-in sessions** are random tokens in an HttpOnly, SameSite=Strict
  cookie (page scripts can't read it; other sites can't use it). The
  server stores only a hash of each token. Sessions end on sign-out, after
  30 minutes without activity, after 12 hours at most, and when the user is
  disabled, their password is reset, or they change it (their other
  sessions). Users see where they are signed in and can sign out elsewhere;
  admins can sign a user out everywhere.
- **Two-step sign-in** with an authenticator app (Google / Microsoft
  Authenticator, Authy), with one-time recovery codes. Admins can require it
  for everyone (Company settings → Security). The app keys are stored
  encrypted with APP_SECRET; recovery codes only as hashes.
- **Passwords**: at least 10 characters with letters and numbers; common
  passwords and ones containing the person's name or email are refused;
  stored with bcrypt (cost 12). 5 wrong attempts for an email or from an
  address → 15-minute pause. Sign-in takes the same time whether or not an
  email exists. Temporary passwords must be changed before anything else —
  enforced by the server.
- **Every failed and successful sign-in is in the audit log**, with the IP.
  The database itself refuses to change or delete audit-log rows.
- **Cross-site request protection**: anything that changes data must come
  from the app's own pages (custom header + Origin / Sec-Fetch-Site checks).
- **Browser hardening headers**: Content-Security-Policy (only the app's own
  scripts), no framing, no sniffing, no referrer, no caching of data.
- **Limits**: request size (1 MB; 10 MB with a slip), connection timeouts,
  page sizes; HTTPS with TLS 1.2 or newer only.
- **Uploads**: slips are checked by content (JPG, PNG, WEBP, PDF only),
  stored under random names, served only to signed-in users, never run.
- **Permissions** are checked on the server for every request; roles
  re-read on every request. Nothing is deleted; every change is logged.

## Install checklist

### 1. Secrets (.env)
- Set **APP_SECRET** to a long random value (the program won't start
  without one). PowerShell (uses Windows' secure random generator):
  `$b = New-Object byte[] 32; [Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b); -join ($b | % { $_.ToString('x2') })`
- Remove the old `JWT_SECRET` line if it still has the example value.
- After the first start, **remove BILLING_ADMIN_PASSWORD** (and the email).
- Lock the file to the account that runs the program and administrators:
  `icacls .env /inheritance:r /grant:r "%USERNAME%:F" "Administrators:F"`

### 2. Database user
- Run `scripts\db-setup.sql` (instructions inside) so the program uses its
  own limited user `apex_billing_app`, not `postgres`.
- If PostgreSQL is on the same computer, keep it listening only there
  (`listen_addresses = 'localhost'` in postgresql.conf) and keep port 5432
  closed in Windows Firewall.
- In pg_hba.conf use `scram-sha-256` (not `trust`).

### 3. HTTPS — required when anyone else on the network uses it
Without HTTPS, passwords and billing data cross the network in plain text.
Pick one:
- **Only this computer uses it**: `LISTEN_ADDR=127.0.0.1` (nothing else can
  connect) and open http://localhost:8090.
- **The program serves HTTPS**: get a certificate for the server's name
  (from your IT / an internal CA, or `mkcert` for a small office), save as
  `tls\cert.pem` and `tls\key.pem`, set `TLS_ENABLED=true` and
  `PORT=443` (or 8443), and open https://servername:port.
- **A reverse proxy serves HTTPS** (IIS with URL Rewrite/ARR, Caddy,
  nginx): set `LISTEN_ADDR=127.0.0.1`, `TRUSTED_PROXIES=127.0.0.1`,
  `COOKIE_SECURE=true`.

### 4. The server computer
- Windows Firewall: allow only the app's port, only from your office
  network.
- Turn on **BitLocker** (disk encryption) — the database, slips and backups
  are then unreadable if the disk or PC is stolen.
- Windows Update on. Only admins can sign in to the server.
- Run the program as a Windows service under its own limited account
  (e.g. with NSSM), so it starts after a reboot without anyone signed in.

### 5. In the app
- Admin → **My account → Two-step sign-in → Set up**; then
  **Company settings → Security → Require two-step sign-in for everyone**.
- Give each person the lowest role that fits (a cashier is a Payment
  Recorder, not an Accountant). Never share accounts.
- Give temporary passwords in person or by phone, not by email.

### 6. Backups
- `scripts\backup.bat` saves the database and slips in a dated folder
  (default `D:\apex-billing-backups`, set BACKUP_DIR to change) and keeps 30.
- The database password goes in
  `%APPDATA%\postgresql\pgpass.conf` (one line:
  `localhost:5432:apex_billing:apex_billing_app:THE-PASSWORD`) of the account
  that runs the backup; lock it with icacls like .env.
- Run it daily — Task Scheduler (as administrator):
  `schtasks /Create /TN "APEX Billing backup" /SC DAILY /ST 22:00 /RU SYSTEM /TR "\"C:\path\to\apex-billing\scripts\backup.bat\""`
  (with /RU SYSTEM put pgpass.conf in
  `C:\Windows\System32\config\systemprofile\AppData\Roaming\postgresql\`).
- Copy backups off the server too (another drive, or an encrypted USB disk
  kept elsewhere). A backup on the same disk doesn't survive a disk failure
  or ransomware.
- **Test a restore** every month on a spare database:
  `createdb -U postgres apex_billing_test` then
  `pg_restore -U postgres -d apex_billing_test D:\apex-billing-backups\<date>\apex_billing.dump`
  and copy the `uploads` folder back next to the program if needed.

## Routine
- **Weekly**: Audit log → look for `login_failed`, `login_blocked`,
  `2fa_reset`, `password_reset`; check backups ran.
- **When someone leaves**: disable their user the same day (their sessions
  end at once).
- **Monthly**: test a restore; update Windows and PostgreSQL; check for
  updates to the program's components:
  `cd frontend && npm audit` and `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`
