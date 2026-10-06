#!/bin/sh
# Shows each candidate for the built-in greetings and asks whether to keep it.
# Run it in a real terminal from the repo root. Answers are saved as you go, so
# you can stop with Ctrl-C and pick up where you left off.
set -eu

export XDG_DATA_HOME="$PWD/tmp/builtin/data" XDG_CONFIG_HOME="$PWD/tmp/builtin/config"
mootd="$PWD/bin/mootd"
pool="$XDG_DATA_HOME/mootd/pool.json"

while :; do
  left=$(jq '[.entries[] | select(.kept != true)] | length' "$pool")
  kept=$(jq '[.entries[] | select(.kept == true)] | length' "$pool")
  [ "$left" -gt 0 ] || break
  # Marking a generation as just attempted stops mootd starting one in this scratch pool.
  jq --arg now "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '.generation.last_attempt = $now' "$pool" > "$pool.tmp"
  mv "$pool.tmp" "$pool"
  clear
  MOOTD_SKIP= "$mootd"
  printf '%s kept, %s left to judge.  Keep this one? [y/n] ' "$kept" "$left"
  read -r answer
  case $answer in
    y | Y) "$mootd" keep >/dev/null ;;
    *) "$mootd" nope >/dev/null ;;
  esac
done
echo "Done: $kept kept."
