#!/usr/bin/env bash
# APEX Billing: update the running app on the server to the newest code.
# Run on the server as root (deploy.bat on the PC does this over SSH):
#   cd /root/apex-billing && git pull --ff-only && bash scripts/update-server.sh
#
# Steps: tests -> build -> backup -> install -> restart -> health check.
# If the new version doesn't come up, the previous one is put back.
set -euo pipefail

REPO=/root/apex-billing
APP=/opt/apex-billing
SERVICE=apex-billing
export GOTOOLCHAIN=auto

# deploy.bat runs this over a plain SSH command, which doesn't load the
# login settings (~/.profile, ~/.bashrc), so Node (nvm) and Go may not be
# on PATH. Find them here.
export PATH="/usr/local/go/bin:/root/go/bin:/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin:$PATH"
export NVM_DIR="${NVM_DIR:-/root/.nvm}"
if ! command -v npm >/dev/null 2>&1 && [ -s "$NVM_DIR/nvm.sh" ]; then
  set +u; . "$NVM_DIR/nvm.sh" >/dev/null; nvm use default >/dev/null 2>&1 || true; set -u
fi
if ! command -v npm >/dev/null 2>&1 && [ -d "$NVM_DIR/versions/node" ]; then
  NODE_BIN=$(ls -d "$NVM_DIR"/versions/node/*/bin 2>/dev/null | sort -V | tail -n 1)
  [ -n "$NODE_BIN" ] && export PATH="$NODE_BIN:$PATH"
fi
command -v npm >/dev/null 2>&1 || { printf '\nDEPLOY FAILED: npm not found on the server (run: which npm, in an SSH window)\n' >&2; exit 1; }
command -v go  >/dev/null 2>&1 || { printf '\nDEPLOY FAILED: go not found on the server (run: which go, in an SSH window)\n' >&2; exit 1; }

step() { printf '\n==> %s\n' "$*"; }
fail() { printf '\nDEPLOY FAILED: %s\n' "$*" >&2; exit 1; }

# Only one deploy at a time.
exec 9>/run/apex-billing-deploy.lock
flock -n 9 || fail "another deploy is running"

cd "$REPO"
step "Code: $(git log -1 --format='%h  %s  (%an, %ad)' --date=format:'%d %b %H:%M')"

step "Installing web page packages"
(cd frontend && npm ci --no-audit --no-fund --loglevel=error) || fail "npm ci"

step "Running the tests"
go test ./internal/... > /tmp/apex-billing-tests.log 2>&1 || { cat /tmp/apex-billing-tests.log; fail "tests failed - nothing was changed on the live app"; }
echo "tests passed"

step "Building"
bash build.sh || fail "build failed - nothing was changed on the live app"

step "Backing up the database and uploads"
/usr/local/sbin/apex-billing-backup || fail "backup failed - nothing was changed on the live app"

# Keep the service file and backup script in step with the code.
if ! cmp -s deploy/apex-billing.service /etc/systemd/system/$SERVICE.service; then
  step "Updating the service settings"
  cp deploy/apex-billing.service /etc/systemd/system/$SERVICE.service
  systemctl daemon-reload
fi
if ! cmp -s scripts/backup.sh /usr/local/sbin/apex-billing-backup; then
  install -m 750 scripts/backup.sh /usr/local/sbin/apex-billing-backup
fi

step "Installing the new version"
cp -p "$APP/apex-billing" "$APP/apex-billing.prev"
install -m 755 -o root -g root apex-billing "$APP/apex-billing"
systemctl restart "$SERVICE"

healthy() {
  for _ in $(seq 1 20); do
    sleep 1
    if curl -fsk --max-time 3 https://127.0.0.1/api/health | grep -q '"ok"'; then return 0; fi
  done
  return 1
}

if healthy; then
  git rev-parse HEAD > "$APP/DEPLOYED_COMMIT"
  step "DEPLOYED OK: $(git log -1 --format='%h %s')"
  journalctl -u "$SERVICE" -n 5 --no-pager
  exit 0
fi

echo
echo "The new version did not start. Its log:"
journalctl -u "$SERVICE" -n 25 --no-pager || true
step "Putting the previous version back"
install -m 755 -o root -g root "$APP/apex-billing.prev" "$APP/apex-billing"
systemctl restart "$SERVICE"
if healthy; then
  fail "new version didn't start; the PREVIOUS version is running again (backup made before the attempt)"
fi
fail "new version didn't start and the previous one didn't either - check: journalctl -u $SERVICE -n 50"
