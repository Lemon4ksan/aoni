#!/usr/bin/env bash
# Copyright (c) 2026 Lemon4ksan All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

set -euo pipefail

CHROME_FILE="fingerprint/profiles/chrome/chrome.go"
FIREFOX_FILE="fingerprint/profiles/firefox/firefox.go"
SAFARI_FILE="fingerprint/profiles/safari/safari.go"

if [ ! -f "$CHROME_FILE" ] || [ ! -f "$FIREFOX_FILE" ] || [ ! -f "$SAFARI_FILE" ]; then
    echo "ERROR: Target files not found! Check directory structure."
    echo "  Expected: $CHROME_FILE, $FIREFOX_FILE and $SAFARI_FILE"
    exit 1
fi

sed_i() {
    if [[ "$OSTYPE" == "darwin"* ]]; then
        sed -i '' "$@"
    else
        sed -i "$@"
    fi
}

get_major() { echo "$1" | cut -d. -f1; }
get_major_minor() { echo "$1" | cut -d. -f1-2; }

# ---------- Fetch latest browser versions ----------
echo "Fetching browser versions..."

VERSIONS_OUTPUT=$(go run ./scripts/fetch-versions/ 2>/dev/null || echo "")

CHROME_WIN_RAW=$(echo "$VERSIONS_OUTPUT" | sed -n 's/^CHROME_WIN=\(.*\)/\1/p')
CHROME_ANDROID_RAW=$(echo "$VERSIONS_OUTPUT" | sed -n 's/^CHROME_ANDROID=\(.*\)/\1/p')
CHROME_IOS_RAW=$(echo "$VERSIONS_OUTPUT" | sed -n 's/^CHROME_IOS=\(.*\)/\1/p')
FIREFOX_RAW=$(echo "$VERSIONS_OUTPUT" | sed -n 's/^FIREFOX=\(.*\)/\1/p')

if [ -z "$CHROME_WIN_RAW" ]; then
    echo "ERROR: Failed to fetch Chrome version."
    exit 1
fi

if [ -z "$FIREFOX_RAW" ]; then
    echo "ERROR: Failed to fetch Firefox version."
    exit 1
fi

CHROME_MAJOR=$(get_major "$CHROME_WIN_RAW")
CHROME_WIN_BUILD=$(echo "$CHROME_WIN_RAW" | cut -d. -f3-)
CHROME_ANDROID_BUILD=$(echo "$CHROME_ANDROID_RAW" | cut -d. -f3-)
CHROME_IOS_BUILD=$(echo "$CHROME_IOS_RAW" | cut -d. -f3-)

echo "Chrome major: $CHROME_MAJOR (win=$CHROME_WIN_RAW android=$CHROME_ANDROID_RAW ios=$CHROME_IOS_RAW)"

FIREFOX_MAJOR_MINOR=$(get_major_minor "$FIREFOX_RAW")

echo "Firefox: $FIREFOX_RAW (major.minor=$FIREFOX_MAJOR_MINOR)"

echo "Fetching iOS version..."
IPSW_JSON=$(curl -sSkL -A "Mozilla/5.0" "https://api.ipsw.me/v4/device/iPhone16,2" 2>/dev/null || echo "")
IOS_FULL=$(echo "$IPSW_JSON" | jq -r '[.firmwares[] | select(.signed == true)] | .[0].version' 2>/dev/null || echo "")
if [ -n "$IOS_FULL" ]; then
    IOS_UA=$(echo "$IOS_FULL" | tr '.' '_')
    echo "iOS: $IOS_FULL (UA format: $IOS_UA)"
else
    IOS_UA=""
    echo "Warning: could not fetch iOS version — iOS OS strings will not be updated."
fi

echo "Fetching Android version (for Firefox UA)..."
WIKI_JSON=$(curl -sSkL -A "Mozilla/5.0" "https://www.wikidata.org/w/api.php?action=wbgetentities&format=json&ids=Q94&props=claims" 2>/dev/null || echo "")
ANDROID_MAJOR=$(echo "$WIKI_JSON" | jq -r '[.entities.Q94.claims.P348[] | select(.rank == "preferred") | .mainsnak.datavalue.value] | sort_by(split(".") | map(tonumber)) | last | split(".") | .[0]' 2>/dev/null || echo "")

# ---------- Read current versions directly from User-Agent strings ----------
CURRENT_CHROME_MAJOR=$(sed -n 's/.*Chrome\/\([0-9]*\)\..*/\1/p' "$CHROME_FILE" | head -1)
CURRENT_CHROME_WIN_BUILD=$(sed -n 's/.*Chrome\/[0-9]*\.0\.\([0-9]*\.[0-9]*\).*/\1/p' "$CHROME_FILE" | head -1 || echo "")

CURRENT_FIREFOX_MAJORMINOR=$(sed -n 's/.*Firefox\/\([0-9]*\.[0-9]*\).*/\1/p' "$FIREFOX_FILE" | head -1)
CURRENT_IOS_CHROME=$(sed -n 's/.*iPhone; CPU iPhone OS \([0-9_]*\).*/\1/p' "$CHROME_FILE" | head -1 || echo "")
CURRENT_IOS_FIREFOX=$(sed -n 's/.*iPhone; CPU iPhone OS \([0-9_]*\).*/\1/p' "$FIREFOX_FILE" | head -1 || echo "")
CURRENT_ANDROID_FIREFOX=$(sed -n 's/.*Android \([0-9]*\);.*/\1/p' "$FIREFOX_FILE" | head -1 || echo "")

CURRENT_IOS_SAFARI=$(sed -n 's/.*iPhone; CPU iPhone OS \([0-9_]*\).*/\1/p' "$SAFARI_FILE" | head -1 || echo "")
CURRENT_SAFARI_VERSION=$(sed -n 's/.*Version\/\([0-9]*\.[0-9]*\).*/\1/p' "$SAFARI_FILE" | head -1 || echo "")

echo ""
echo "Current Chrome: $CURRENT_CHROME_MAJOR (win build=$CURRENT_CHROME_WIN_BUILD)"
echo "Current Firefox: $CURRENT_FIREFOX_MAJORMINOR"
echo "Current Safari: $CURRENT_SAFARI_VERSION"
echo "Current iOS (Chrome UA): $CURRENT_IOS_CHROME | (Firefox UA): $CURRENT_IOS_FIREFOX | (Safari UA): $CURRENT_IOS_SAFARI"
echo "Current Android (Firefox UA): $CURRENT_ANDROID_FIREFOX"
echo ""

UPDATED=false

# ---------- Update Chrome User-Agent and Sec-CH-UA Strings ----------
if [ "$CHROME_MAJOR" != "$CURRENT_CHROME_MAJOR" ]; then
    echo "Updating Chrome strings: $CURRENT_CHROME_MAJOR -> $CHROME_MAJOR"
    UPDATED=true

    # Update Sec-CH-UA header
    sed_i "s/\"Google Chrome\";v=\"${CURRENT_CHROME_MAJOR}\"/\"Google Chrome\";v=\"${CHROME_MAJOR}\"/g" "$CHROME_FILE"
    sed_i "s/\"Chromium\";v=\"${CURRENT_CHROME_MAJOR}\"/\"Chromium\";v=\"${CHROME_MAJOR}\"/g" "$CHROME_FILE"

    # Update User-Agent strings
    sed_i "s/Chrome\/${CURRENT_CHROME_MAJOR}\.0\.0\.0/Chrome\/${CHROME_MAJOR}.0.0.0/g" "$CHROME_FILE"

    if [ -n "$CURRENT_CHROME_WIN_BUILD" ] && [ -n "$CHROME_WIN_BUILD" ]; then
        sed_i "s/Chrome\/${CURRENT_CHROME_MAJOR}\.0\.${CURRENT_CHROME_WIN_BUILD}/Chrome\/${CHROME_MAJOR}.0.${CHROME_WIN_BUILD}/g" "$CHROME_FILE"
    fi

    sed_i "s/Chrome\/${CURRENT_CHROME_MAJOR}\.0\.[0-9]*\.[0-9]*/Chrome\/${CHROME_MAJOR}.0.${CHROME_ANDROID_BUILD}/g" "$CHROME_FILE"
    sed_i "s/CriOS\/${CURRENT_CHROME_MAJOR}\.0\.[0-9]*\.[0-9]*/CriOS\/${CHROME_MAJOR}.0.${CHROME_IOS_BUILD}/g" "$CHROME_FILE"

    echo "Chrome updated."
else
    echo "Chrome is up to date ($CURRENT_CHROME_MAJOR)."
fi

# ---------- Update Firefox User-Agent Strings ----------
if [ "$FIREFOX_MAJOR_MINOR" != "$CURRENT_FIREFOX_MAJORMINOR" ]; then
    echo "Updating Firefox strings: $CURRENT_FIREFOX_MAJORMINOR -> $FIREFOX_MAJOR_MINOR"
    UPDATED=true

    sed_i "s/rv:${CURRENT_FIREFOX_MAJORMINOR}/rv:${FIREFOX_MAJOR_MINOR}/g" "$FIREFOX_FILE"
    sed_i "s/Firefox\/${CURRENT_FIREFOX_MAJORMINOR}/Firefox\/${FIREFOX_MAJOR_MINOR}/g" "$FIREFOX_FILE"
    sed_i "s/FxiOS\/${CURRENT_FIREFOX_MAJORMINOR}/FxiOS\/${FIREFOX_MAJOR_MINOR}/g" "$FIREFOX_FILE"

    echo "Firefox updated."
else
    echo "Firefox is up to date ($CURRENT_FIREFOX_MAJORMINOR)."
fi

# ---------- Update Safari User-Agent Strings ----------
SAFARI_VERSION=""
if [ -n "$IOS_FULL" ]; then
    SAFARI_VERSION=$(get_major_minor "$IOS_FULL")
    if [ -n "$CURRENT_SAFARI_VERSION" ] && [ "$SAFARI_VERSION" != "$CURRENT_SAFARI_VERSION" ]; then
        echo "Updating Safari strings: $CURRENT_SAFARI_VERSION -> $SAFARI_VERSION"
        UPDATED=true

        sed_i "s/Version\/${CURRENT_SAFARI_VERSION}/Version\/${SAFARI_VERSION}/g" "$SAFARI_FILE"

        echo "Safari updated."
    else
        echo "Safari is up to date ($CURRENT_SAFARI_VERSION)."
    fi
fi

# ---------- Update iOS OS version ----------
if [ -n "$IOS_UA" ]; then
    if [ -n "$CURRENT_IOS_CHROME" ] && [ "$IOS_UA" != "$CURRENT_IOS_CHROME" ]; then
        echo "Updating Chrome iOS OS: $CURRENT_IOS_CHROME -> $IOS_UA"
        sed_i "s/iPhone; CPU iPhone OS ${CURRENT_IOS_CHROME}/iPhone; CPU iPhone OS ${IOS_UA}/g" "$CHROME_FILE"
        UPDATED=true
    fi

    if [ -n "$CURRENT_IOS_FIREFOX" ] && [ "$IOS_UA" != "$CURRENT_IOS_FIREFOX" ]; then
        echo "Updating Firefox iOS OS: $CURRENT_IOS_FIREFOX -> $IOS_UA"
        sed_i "s/iPhone; CPU iPhone OS ${CURRENT_IOS_FIREFOX}/iPhone; CPU iPhone OS ${IOS_UA}/g" "$FIREFOX_FILE"
        UPDATED=true
    fi

    if [ -n "$CURRENT_IOS_SAFARI" ] && [ "$IOS_UA" != "$CURRENT_IOS_SAFARI" ]; then
        echo "Updating Safari iOS OS: $CURRENT_IOS_SAFARI -> $IOS_UA"
        sed_i "s/iPhone; CPU iPhone OS ${CURRENT_IOS_SAFARI}/iPhone; CPU iPhone OS ${IOS_UA}/g" "$SAFARI_FILE"
        UPDATED=true
    fi
fi

# ---------- Update Android OS version ----------
if [ -n "$ANDROID_MAJOR" ] && [ -n "$CURRENT_ANDROID_FIREFOX" ]; then
    if [ "$ANDROID_MAJOR" != "$CURRENT_ANDROID_FIREFOX" ]; then
        echo "Updating Firefox Android OS: $CURRENT_ANDROID_FIREFOX -> $ANDROID_MAJOR"
        sed_i "s/Android ${CURRENT_ANDROID_FIREFOX};/Android ${ANDROID_MAJOR};/g" "$FIREFOX_FILE"
        UPDATED=true
    fi
fi

# ---------- Update utls dependency ----------
echo ""
echo "Checking utls dependency..."

UTLS_UPDATED=false
UTLS_LATEST=""

if command -v go &>/dev/null; then
    UTLS_CURRENT=$(grep 'refraction-networking/utls' go.mod | awk '{print $2}')
    UTLS_LATEST=$(go list -m github.com/refraction-networking/utls@latest 2>/dev/null | awk '{print $2}' || echo "")

    if [ -n "$UTLS_LATEST" ] && [ "$UTLS_LATEST" != "$UTLS_CURRENT" ]; then
        echo "Updating utls: $UTLS_CURRENT -> $UTLS_LATEST"
        go get "github.com/refraction-networking/utls@${UTLS_LATEST}"
        go mod tidy
        UPDATED=true
        UTLS_UPDATED=true
        echo "utls updated."
    else
        UTLS_LATEST="${UTLS_CURRENT}"
        echo "utls is up to date (${UTLS_CURRENT})."
    fi
fi

# ---------- Compare TLS ClientHello specs against utls auto ----------
echo ""
echo "Comparing TLS ClientHello specs against utls.HelloChrome_Auto / HelloFirefox_Auto / HelloSafari_Auto..."

SPEC_DRIFT=false
SPEC_DIFF_OUTPUT=""

if command -v go &>/dev/null; then
    set +e
    SPEC_DIFF_OUTPUT=$(go run ./scripts/compare-tls-spec/ 2>&1)
    SPEC_EXIT=$?
    set -e

    echo "$SPEC_DIFF_OUTPUT"

    if [ "$SPEC_EXIT" -eq 1 ]; then
        echo ""
        echo "⚠️  TLS spec drift detected."
        SPEC_DRIFT=true
        UPDATED=true
    elif [ "$SPEC_EXIT" -eq 0 ]; then
        echo "TLS specs are in sync."
    fi
fi

# ---------- GitHub Actions outputs ----------
if [ -n "${GITHUB_OUTPUT:-}" ]; then
    echo "updated=$UPDATED"                            >> "$GITHUB_OUTPUT"
    echo "chrome_version=$CHROME_MAJOR"                >> "$GITHUB_OUTPUT"
    echo "chrome_win_version=$CHROME_WIN_RAW"          >> "$GITHUB_OUTPUT"
    echo "chrome_android_version=$CHROME_ANDROID_RAW"  >> "$GITHUB_OUTPUT"
    echo "chrome_ios_version=$CHROME_IOS_RAW"          >> "$GITHUB_OUTPUT"
    echo "firefox_version=$FIREFOX_RAW"                >> "$GITHUB_OUTPUT"
    echo "safari_version=${SAFARI_VERSION:-$CURRENT_SAFARI_VERSION}" >> "$GITHUB_OUTPUT"
    echo "utls_version=${UTLS_LATEST:-unknown}"        >> "$GITHUB_OUTPUT"
    echo "utls_updated=${UTLS_UPDATED}"                >> "$GITHUB_OUTPUT"
    echo "spec_drift=${SPEC_DRIFT}"                    >> "$GITHUB_OUTPUT"
    echo "ios_version=${IOS_FULL:-unknown}"            >> "$GITHUB_OUTPUT"
    echo "android_version=${ANDROID_MAJOR:-unknown}"   >> "$GITHUB_OUTPUT"

    {
        echo "spec_diff_output<<EOF_SPEC_DIFF"
        echo "$SPEC_DIFF_OUTPUT"
        echo "EOF_SPEC_DIFF"
    } >> "$GITHUB_OUTPUT"
fi

# ---------- Dry run ----------
if [ "${1:-}" = "--dry-run" ]; then
    echo ""
    echo "=== DRY RUN ==="
    git diff --stat
    git diff
    echo "=== END DRY RUN ==="
    exit 0
fi

# ---------- Verify build, tests & live canary ----------
if [ "$UPDATED" = "true" ]; then
    echo ""
    echo "Verifying Go build..."
    if command -v go &>/dev/null; then
        if ! go build ./...; then
            echo "ERROR: Build failed! Reverting..."
            git checkout -- "$CHROME_FILE" "$FIREFOX_FILE" "$SAFARI_FILE" go.mod go.sum 2>/dev/null || true
            echo "updated=false" >> "${GITHUB_OUTPUT:-/dev/null}"
            exit 1
        fi
        echo "Build OK."

        echo "Running profile tests..."
        if ! go test ./fingerprint/profiles/...; then
            echo "ERROR: Profile tests failed! Reverting..."
            git checkout -- "$CHROME_FILE" "$FIREFOX_FILE" "$SAFARI_FILE" go.mod go.sum 2>/dev/null || true
            echo "updated=false" >> "${GITHUB_OUTPUT:-/dev/null}"
            exit 1
        fi
        echo "Tests OK."

        echo "Running Live Canary Test against tls.peet.ws..."
        if ! go run ./scripts/canary-test/; then
            echo "ERROR: Canary test failed! Reverting..."
            git checkout -- "$CHROME_FILE" "$FIREFOX_FILE" "$SAFARI_FILE" go.mod go.sum 2>/dev/null || true
            echo "updated=false" >> "${GITHUB_OUTPUT:-/dev/null}"
            exit 1
        fi
        echo "Canary Test OK."
    fi
fi
