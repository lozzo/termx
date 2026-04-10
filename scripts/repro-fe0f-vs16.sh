#!/usr/bin/env bash
set -euo pipefail

mode="${1:-implicit}"
case "$mode" in
  implicit)
    ech=$'\033[X'
    label='ECH default (\033[X)'
    ;;
  explicit)
    ech=$'\033[1X'
    label='ECH explicit (\033[1X)'
    ;;
  none)
    ech=''
    label='no ECH'
    ;;
  *)
    printf 'usage: %s [implicit|explicit|none]\n' "$0" >&2
    exit 2
    ;;
esac

cleanup() {
  printf '\033[0m\033[?25h\033[?1049l'
}
trap cleanup EXIT INT TERM

printf '\033[?1049h\033[2J\033[H\033[?25l'
printf 'termx FE0F repro: %s\r\n' "$label"
printf 'Row 3 uses U+2744 U+FE0F (❄️); row 4 uses U+267B U+FE0F (♻️).\r\n'
printf 'Sequence: CHA(1) + AAA + emoji + ECH? + CHA(6) + BB + border + EL.\r\n'
printf '\r\n'
printf '\033[5;1H\033[1GAAA❄️%s\033[6GBB│\033[0m\033[K' "$ech"
printf '\033[6;1H\033[1GAAA♻️%s\033[6GBB│\033[0m\033[K' "$ech"
printf '\033[8;1HPress any key to exit...'
IFS= read -r -n 1 _
