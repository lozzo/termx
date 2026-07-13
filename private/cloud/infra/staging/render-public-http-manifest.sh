#!/bin/sh
set -eu

input=${1:-/var/lib/termx-staging/runtime.json}
output=${2:-/var/www/termx-staging/runtime.json}
temporary="${output}.tmp"

jq '
  .profile = "staging-public-http"
  | .control_plane_url = "http://114.66.58.243:41101"
  | .hub_url = "http://114.66.58.243:41102"
  | .enrollment_code = "public-client-enrollment-disabled"
' "$input" >"$temporary"
chmod 0644 "$temporary"
mv "$temporary" "$output"
