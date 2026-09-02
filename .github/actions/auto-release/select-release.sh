#!/usr/bin/env bash
set -euo pipefail

is_new_release() {
  [[ "$1" == "true" && "$2" != "none" && "$3" == ">" ]]
}

write_output() {
  echo "$1=$2" >>"${GITHUB_OUTPUT}"
}

app_release=false
chart_release=false
is_new_release "${APP_CHANGED}" "${APP_TYPE}" "${APP_COMPARISON}" && app_release=true
is_new_release "${CHART_CHANGED}" "${CHART_TYPE}" "${CHART_COMPARISON}" && chart_release=true

if [[ "${app_release}" == "false" && "${chart_release}" == "false" ]]; then
  echo "No application or chart release is pending."
  write_output release false
  exit 0
fi

if [[ "${app_release}" == "true" && "${chart_release}" != "true" ]]; then
  echo "An application release must also bump the chart version." >&2
  exit 1
fi

release_chart_version="${CHART_VERSION}"
if [[ "${app_release}" == "true" && "${APP_CHART_COMPARISON}" == ">" ]]; then
  release_chart_version="${APP_VERSION}"
fi

write_output release true
write_output app_release "${app_release}"
write_output chart "${release_chart_version}"
if [[ "${app_release}" == "true" ]]; then
  write_output kind app
  write_output app "${APP_VERSION}"
else
  write_output kind chart
  write_output app "${CURRENT_APP_VERSION}"
fi
