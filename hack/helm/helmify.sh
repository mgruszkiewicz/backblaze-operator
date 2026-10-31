#!/usr/bin/env bash
# Regenerate the Helm chart from the kustomize manifests without losing the
# manual customisations that helmify does not know about.
#
# helmify always produces the "vanilla" chart. Our customisations on top of it
# (existing-secret support, documented values, ...) live in hack/helm/chart.patch
# and are re-applied after every run. Files listed in PROTECTED_FILES are fully
# hand-maintained and are never touched.
#
# Usage:
#   hack/helm/helmify.sh generate   # regenerate the chart in $HELM_CHART_DIR
#   hack/helm/helmify.sh patch      # record current manual edits in chart.patch
#
# Workflow for a new manual customisation:
#   1. make helm            (chart is in sync with the operator manifests)
#   2. edit the chart by hand in $HELM_CHART_DIR
#   3. make helm-patch      (stores the edit in hack/helm/chart.patch, commit it)
#
# Environment:
#   HELM_CHART_DIR  path to the chart (default: ../helm-charts/charts/backblaze-operator)
#   KUSTOMIZE       kustomize binary (default: bin/kustomize)
#   HELMIFY         helmify binary (default: helmify)
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PATCH_FILE="${ROOT_DIR}/hack/helm/chart.patch"
CHART_NAME="backblaze-operator"
HELM_CHART_DIR="${HELM_CHART_DIR:-${ROOT_DIR}/../helm-charts/charts/${CHART_NAME}}"
KUSTOMIZE="${KUSTOMIZE:-${ROOT_DIR}/bin/kustomize}"
HELMIFY="${HELMIFY:-helmify}"

# Hand-maintained files: never generated, never overwritten, never deleted.
PROTECTED_FILES=(
  Chart.yaml
  README.md
  artifacthub-pkg.yml
  .helmignore
  templates/_helpers.tpl
)

die() { echo "error: $*" >&2; exit 1; }

command -v "${HELMIFY}" >/dev/null 2>&1 || die "helmify not found (brew install arttor/tap/helmify or go install github.com/arttor/helmify/cmd/helmify@latest)"
[ -x "${KUSTOMIZE}" ] || die "kustomize not found at ${KUSTOMIZE} (run: make kustomize)"
[ -d "${HELM_CHART_DIR}" ] || die "chart directory ${HELM_CHART_DIR} does not exist (set HELM_CHART_DIR)"
HELM_CHART_DIR="$(cd "${HELM_CHART_DIR}" && pwd)"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT

# Render the vanilla helmify chart into $WORK_DIR/$CHART_NAME.
render_vanilla() {
  (cd "${ROOT_DIR}" && "${KUSTOMIZE}" build config/default) | (cd "${WORK_DIR}" && "${HELMIFY}" "${CHART_NAME}")
  for f in "${PROTECTED_FILES[@]}"; do
    rm -f "${WORK_DIR}/${CHART_NAME}/${f}"
  done
}

RSYNC_EXCLUDES=()
for f in "${PROTECTED_FILES[@]}"; do
  RSYNC_EXCLUDES+=("--exclude=/${f}")
done

cmd_generate() {
  render_vanilla
  local gen="${WORK_DIR}/${CHART_NAME}"

  if [ -s "${PATCH_FILE}" ]; then
    echo "Applying customisations from ${PATCH_FILE#"${ROOT_DIR}"/}"
    if ! patch -s -N -p1 -d "${gen}" < "${PATCH_FILE}"; then
      die "chart.patch no longer applies cleanly to the helmify output.
The chart in ${HELM_CHART_DIR} was NOT modified.
Update the customisations by hand in the chart, then run 'make helm-patch'."
    fi
    find "${gen}" \( -name '*.orig' -o -name '*.rej' \) -delete
  fi

  # --delete removes templates helmify no longer produces; protected files are excluded
  # from both copying and deletion.
  rsync -a --delete --checksum "${RSYNC_EXCLUDES[@]}" "${gen}/" "${HELM_CHART_DIR}/"

  if command -v helm >/dev/null 2>&1; then
    helm lint "${HELM_CHART_DIR}" \
      --set credentials.b2ApplicationId=lint \
      --set credentials.b2ApplicationKey=lint \
      --set credentials.b2Region=lint
  fi
  if command -v helm-docs >/dev/null 2>&1; then
    helm-docs --chart-search-root "${HELM_CHART_DIR}" >/dev/null
  fi

  echo
  echo "Chart regenerated in ${HELM_CHART_DIR}"
  if git -C "${HELM_CHART_DIR}" rev-parse --git-dir >/dev/null 2>&1; then
    git -C "${HELM_CHART_DIR}" status --short -- .
    echo "Remember to bump 'version' (and 'appVersion' if needed) in Chart.yaml."
  fi
}

cmd_patch() {
  render_vanilla
  local gen="${WORK_DIR}/${CHART_NAME}"
  mkdir -p "${WORK_DIR}/a" "${WORK_DIR}/b"
  rsync -a "${gen}/" "${WORK_DIR}/a/"
  rsync -a "${RSYNC_EXCLUDES[@]}" "${HELM_CHART_DIR}/" "${WORK_DIR}/b/"

  # diff exits 1 when files differ, which is the expected case here. Timestamps
  # and the "diff" command lines are stripped to keep the patch stable in git.
  local rc=0
  (cd "${WORK_DIR}" && diff -ruN a b) > "${WORK_DIR}/raw.patch" || rc=$?
  [ "${rc}" -le 1 ] || die "diff failed"
  awk -F '\t' '/^diff -ruN / { next } /^(---|\+\+\+) / { print $1; next } { print }' \
    "${WORK_DIR}/raw.patch" > "${PATCH_FILE}"
  echo "Recorded customisations in ${PATCH_FILE#"${ROOT_DIR}"/}:"
  grep '^+++ ' "${PATCH_FILE}" | sed -e 's#^+++ b/#  #' || echo "  (none)"
}

case "${1:-generate}" in
  generate) cmd_generate ;;
  patch) cmd_patch ;;
  *) die "unknown command '${1}' (expected: generate | patch)" ;;
esac
