#!/usr/bin/env bash
set -euo pipefail

app_version="$(awk '$1 == "appVersion:" { gsub(/"/, "", $2); print $2 }' charts/avahi-lb/Chart.yaml)"
chart_version="$(awk '$1 == "version:" { print $2; exit }' charts/avahi-lb/Chart.yaml)"

[[ -n "${app_version}" && -n "${chart_version}" ]]
echo "app=${app_version}" >>"${GITHUB_OUTPUT}"
echo "chart=${chart_version}" >>"${GITHUB_OUTPUT}"
