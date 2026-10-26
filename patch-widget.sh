#!/usr/bin/env bash
# Re-applies the tuxedodav redirect after an omarchy-tuxedo-todo plugin update
# (the update overwrites Tasks.qml and restores the plain "tuxedo" command).
set -euo pipefail
QML="$HOME/.config/omarchy/plugins/omarchy-tuxedo/Tasks.qml"
BIN="$HOME/.local/bin/tuxedodav"
[ -f "$QML" ] || { echo "plugin Tasks.qml not found"; exit 1; }
[ -x "$BIN" ] || { echo "tuxedodav binary not found at $BIN"; exit 1; }
[ -f "$QML.orig" ] || cp "$QML" "$QML.orig"
if grep -q "local/bin/tuxedodav" "$QML"; then
  echo "already patched"
else
  sed -i 's|"tuxedo"|"'"$BIN"'"|' "$QML"
  echo "patched: " $(grep -c "local/bin/tuxedodav" "$QML") "occurrence(s)"
fi