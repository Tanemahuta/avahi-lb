#!/usr/bin/env bash
set -euo pipefail

ensure_label_exists() {
  if gh label view "${REVIEW_LABEL}" --repo "${REPOSITORY}" >/dev/null 2>&1; then
    return
  fi

  gh label create "${REVIEW_LABEL}" \
    --repo "${REPOSITORY}" \
    --description "Dependabot update requiring manual review" \
    --color B60205
}

pr_has_label() {
  gh pr view "${PR_URL}" --json labels \
    --jq '.labels | any(.name == env.REVIEW_LABEL)'
}

ensure_label_exists
if [[ "$(pr_has_label)" != "true" ]]; then
  gh pr edit "${PR_URL}" --add-label "${REVIEW_LABEL}"
  gh pr comment "${PR_URL}" --body \
    "Dependabot update ${DEPENDABOT_UPDATE:-unknown} for ${DEPENDABOT_DEPENDENCY_TYPE:-unknown} dependencies requires manual review."
fi
