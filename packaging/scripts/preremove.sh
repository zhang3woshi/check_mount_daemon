#!/bin/sh
set -e

if command -v systemctl >/dev/null 2>&1; then
  case "$1" in
    remove)
      systemctl stop check-mount.service || true
      systemctl disable check-mount.service || true
      ;;
    upgrade|deconfigure)
      systemctl stop check-mount.service || true
      ;;
  esac
fi
