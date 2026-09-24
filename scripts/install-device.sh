#!/usr/bin/env bash
# Build and install Relay on a connected iPhone with a free (personal) team.
# Free-team builds expire after 7 days; rerun this to renew.
#   TEAM=<team id> scripts/install-device.sh
set -euo pipefail
: "${TEAM:?set TEAM to your development team id}"
cd "$(dirname "$0")/../ios"
udid=$(xcodebuild -scheme Relay -showdestinations 2>/dev/null | sed -n 's/.*platform:iOS, arch:arm64, id:\([^,]*\),.*/\1/p' | head -1)
[ -n "$udid" ] || { echo "no connected iPhone" >&2; exit 1; }
xcodebuild -scheme Relay -configuration Debug -destination "id=$udid" -derivedDataPath build/device \
  -allowProvisioningUpdates DEVELOPMENT_TEAM="$TEAM" \
  CODE_SIGN_ENTITLEMENTS=Relay/Relay-FreeTeam.entitlements build -quiet
xcrun devicectl device install app --device "$udid" build/device/Build/Products/Debug-iphoneos/Relay.app
echo "installed on $udid"
