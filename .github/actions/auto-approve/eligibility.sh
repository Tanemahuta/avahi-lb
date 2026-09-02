#!/usr/bin/env bash
set -euo pipefail

write_result() {
  echo "eligible=$1" >>"${GITHUB_OUTPUT}"
}

is_latest_commit() {
  local current_head
  current_head="$(gh pr view "${PR_URL}" --json headRefOid --jq .headRefOid)"
  [[ "${current_head}" == "${EVENT_HEAD_SHA}" ]]
}

dependabot_rule_matches() {
  local rule update dependencies dependency
  local -a rules dependency_types

  IFS=';' read -ra rules <<<"${DEPENDABOT_APPROVE}"
  for rule in "${rules[@]}"; do
    IFS='=' read -r update dependencies <<<"${rule}"
    update="${update//[[:space:]]/}"
    dependencies="${dependencies//[[:space:]]/}"

    [[ -n "${update}" &&
       "${DEPENDABOT_UPDATE}" == "version-update:semver-${update}" ]] || continue
    [[ "${dependencies}" == "*" ]] && return 0

    IFS=',' read -ra dependency_types <<<"${dependencies}"
    for dependency in "${dependency_types[@]}"; do
      [[ "${DEPENDABOT_DEPENDENCY_TYPE}" == "${dependency}" ]] && return 0
    done
  done

  return 1
}

is_auto_release() {
  [[ "${HEAD_REPOSITORY}" == "${REPOSITORY}" && "${HEAD_REF}" == release-* ]] &&
    jq -e 'index("auto-release") != null' <<<"${LABELS}" >/dev/null
}

if ! is_latest_commit; then
  echo "The pull request changed while approval was running."
  write_result false
elif [[ "${ACTOR}" == "${REPOSITORY_OWNER}" ]]; then
  write_result true
elif [[ "${ACTOR}" == "dependabot[bot]" ]] && dependabot_rule_matches; then
  write_result true
elif is_auto_release; then
  write_result true
else
  write_result false
fi
