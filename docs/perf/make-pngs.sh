#!/usr/bin/env bash
# Renders the three report charts to PNG via headless Chrome (task 43).
# Usage: ./make-pngs.sh   (from anywhere; writes PNGs next to itself)
set -euo pipefail
cd "$(dirname "$0")"
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
REPORT="$(cd .. && pwd)/perf-report.html"
shot() { # name height
  "$CHROME" --headless --disable-gpu --hide-scrollbars \
    --window-size=1120,"$2" --screenshot="$1.png" \
    "file://$REPORT?png=$1" 2>/dev/null
  echo "wrote $1.png"
}
shot baseline 660
shot stress 660
shot tuning 560
