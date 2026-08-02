#!/usr/bin/env bash
set -euo pipefail

ios_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repo_root="$(cd "${ios_root}/../../.." && pwd)"
developer_dir="${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}"
configuration="${ANYTTY_IOS_CONFIGURATION:-Debug}"
sdk="${ANYTTY_IOS_SDK:-iphonesimulator}"
derived_data="${ANYTTY_IOS_DERIVED_DATA:-${ios_root}/DerivedData}"

if [[ ! -d "${developer_dir}" ]]; then
  echo "Xcode developer directory is unavailable: ${developer_dir}" >&2
  exit 1
fi

case "${sdk}" in
  iphonesimulator)
    destination="${ANYTTY_IOS_DESTINATION:-generic/platform=iOS Simulator}"
    ;;
  iphoneos)
    destination="${ANYTTY_IOS_DESTINATION:-generic/platform=iOS}"
    ;;
  *)
    echo "ANYTTY_IOS_SDK must be iphonesimulator or iphoneos" >&2
    exit 1
    ;;
esac

(
  cd "${repo_root}/clients/mobile"
  node "${repo_root}/clients/mobile/node_modules/typescript/bin/tsc"
  node "${repo_root}/node_modules/vite/bin/vite.js" build
  node scripts/check-bundle-size.mjs
  node scripts/verify-production-bundle.mjs
)
"${ios_root}/scripts/sync-web-assets.sh"
DEVELOPER_DIR="${developer_dir}" "${ios_root}/scripts/build-go-xcframework.sh"

DEVELOPER_DIR="${developer_dir}" xcodebuild \
  -project "${ios_root}/App/App.xcodeproj" \
  -scheme App \
  -configuration "${configuration}" \
  -sdk "${sdk}" \
  -destination "${destination}" \
  -derivedDataPath "${derived_data}" \
  CODE_SIGNING_ALLOWED="${ANYTTY_IOS_CODE_SIGNING_ALLOWED:-NO}" \
  build
