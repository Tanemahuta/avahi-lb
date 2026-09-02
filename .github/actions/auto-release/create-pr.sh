#!/usr/bin/env bash
set -euo pipefail

release_name() {
  if [[ "${RELEASE_KIND}" == "app" ]]; then
    echo "v${APP_VERSION}"
  else
    echo "avahi-lb-${CHART_VERSION}"
  fi
}

ensure_label() {
  local label="$1" description="$2"
  gh label create "${label}" --repo "${GITHUB_REPOSITORY}" \
    --description "${description}" --color 1d76db --force
}

if [[ "$(gh pr list --repo "${GITHUB_REPOSITORY}" --state open \
  --label auto-release --json number --jq 'length > 0')" == "true" ]]; then
  echo "An auto-release PR is already open."
  exit 0
fi

name="$(release_name)"
branch="release-${name}-${GITHUB_RUN_ID}"

sed -i -E "s/^version: .*/version: ${CHART_VERSION}/" charts/avahi-lb/Chart.yaml
if [[ "${RELEASE_APP}" == "true" ]]; then
  sed -i -E "s/^appVersion: .*/appVersion: \"${APP_VERSION}\"/" charts/avahi-lb/Chart.yaml
fi

git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
git switch -c "${branch}"
git add charts/avahi-lb/Chart.yaml
git commit -m "auto-release: ${name}"
git push origin "${branch}"

ensure_label auto-release "Automated release"
ensure_label "auto-release-${RELEASE_KIND}" "Automated ${RELEASE_KIND} release"
gh pr create --repo "${GITHUB_REPOSITORY}" \
  --base main --head "${branch}" \
  --title "auto-release: ${name}" \
  --body "Bumps the application to ${APP_VERSION} and the Helm chart to ${CHART_VERSION}." \
  --label auto-release --label "auto-release-${RELEASE_KIND}"
