#!/usr/bin/env bash
# APEX Billing backup on the Linux server: the database and the uploads
# (slips, logos, contracts) in a dated folder; keeps the newest 30.
# Install (as root):
#   sudo install -m 750 backup.sh /usr/local/sbin/apex-billing-backup
#   echo '30 22 * * * root /usr/local/sbin/apex-billing-backup' | sudo tee /etc/cron.d/apex-billing-backup
# Copy /var/backups/apex-billing somewhere else too (another machine or an
# encrypted USB disk): a backup on the same disk doesn't survive a disk failure.
set -euo pipefail
BACKUP_DIR="${BACKUP_DIR:-/var/backups/apex-billing}"
DB_NAME="${DB_NAME:-apex_billing}"
UPLOADS="${UPLOADS:-/var/lib/apex-billing/uploads}"
KEEP="${KEEP:-30}"

umask 077
out="$BACKUP_DIR/$(date +%Y-%m-%d_%H%M)"
mkdir -p "$out"
# As the postgres system user: no database password needed in a file.
runuser -u postgres -- pg_dump -Fc "$DB_NAME" > "$out/apex_billing.dump"
if [ -d "$UPLOADS" ]; then
  tar -C "$(dirname "$UPLOADS")" -czf "$out/uploads.tar.gz" "$(basename "$UPLOADS")"
fi
# Check the dump can be read.
pg_restore --list "$out/apex_billing.dump" > /dev/null
# Keep the newest $KEEP.
ls -1d "$BACKUP_DIR"/*/ 2>/dev/null | sort -r | tail -n +$((KEEP + 1)) | xargs -r rm -rf
echo "Backup done: $out"
