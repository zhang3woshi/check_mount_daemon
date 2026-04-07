#!/bin/sh
set -e

mkdir -p /etc/check_mount_daemon
chmod 755 /etc/check_mount_daemon

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || true
  systemctl enable check-mount.service || true
  systemctl restart check-mount.service || systemctl start check-mount.service || true
fi
