#!/bin/sh
# Throwaway shell-only skip proof (removed before merge; PR closed unmerged).
# shellcheck: no warnings expected.
if [ -n "${SKIP_PROOF:-}" ]; then
  echo "skip proof"
fi
