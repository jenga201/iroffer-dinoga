#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Build a fresh image and publish it to Docker Hub.

Environment variables:
  DOCKERHUB_REPO        Docker Hub repo (default: jenga201/iroffer-dinoga).
  IMAGE_NAME            Local image name (default: iroffer-dinoga).
  IMAGE_TAG             Base image tag before auto-bump (default: local).
  AUTO_INCREMENT_TAG    Auto-bump IMAGE_TAG each run (default: 1).
  UPDATE_ENV_TAG        Persist bumped IMAGE_TAG to .env (default: 1).
  PUSH_LATEST           Push :latest too (default: 1).
  IROFFER_USER_ID       Build arg (default: 1000).
  IROFFER_GROUP_ID      Build arg (default: 1000).
  DOCKERHUB_USERNAME    Optional; used with DOCKERHUB_TOKEN for non-interactive login.
  DOCKERHUB_TOKEN       Optional Docker Hub access token.

Examples:
  ./publish-dockerhub.sh
  DOCKERHUB_REPO=myorg/iroffer-dinoga IMAGE_TAG=v1.2.3 ./publish-dockerhub.sh
EOF
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

increment_image_tag() {
  local current_tag="$1"
  local prefix major minor patch

  if [[ "${current_tag}" =~ ^(v?)([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    prefix="${BASH_REMATCH[1]}"
    major="${BASH_REMATCH[2]}"
    minor="${BASH_REMATCH[3]}"
    patch="${BASH_REMATCH[4]}"
    patch=$((patch + 1))
    printf '%s%s.%s.%s\n' "${prefix}" "${major}" "${minor}" "${patch}"
    return
  fi

  if [ -z "${current_tag}" ] || [ "${current_tag}" = "local" ]; then
    printf 'v0.0.1\n'
    return
  fi

  echo "IMAGE_TAG '${current_tag}' is not semver-like (expected vX.Y.Z or X.Y.Z)." >&2
  exit 1
}

persist_image_tag_to_env() {
  local new_tag="$1"

  if [ "${UPDATE_ENV_TAG}" != "1" ] || [ ! -f ".env" ]; then
    return
  fi

  if grep -q '^IMAGE_TAG=' ./.env; then
    sed -i -E "s|^IMAGE_TAG=.*$|IMAGE_TAG=${new_tag}|" ./.env
  else
    printf '\nIMAGE_TAG=%s\n' "${new_tag}" >> ./.env
  fi
}

load_env_file() {
  if [ -f ".env" ]; then
    # shellcheck disable=SC1091
    set -a
    . ./.env
    set +a
  fi
}

docker_login_if_requested() {
  if [ -n "${DOCKERHUB_TOKEN:-}" ] || [ -n "${DOCKERHUB_USERNAME:-}" ]; then
    if [ -z "${DOCKERHUB_USERNAME:-}" ] || [ -z "${DOCKERHUB_TOKEN:-}" ]; then
      echo "Both DOCKERHUB_USERNAME and DOCKERHUB_TOKEN are required for scripted login." >&2
      exit 1
    fi

    echo "Logging in to Docker Hub as ${DOCKERHUB_USERNAME}..."
    printf '%s' "${DOCKERHUB_TOKEN}" | docker login --username "${DOCKERHUB_USERNAME}" --password-stdin
  fi
}

main() {
  if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
    usage
    exit 0
  fi

  require_cmd docker
  load_env_file

  IMAGE_NAME="${IMAGE_NAME:-iroffer-dinoga}"
  IMAGE_TAG="${IMAGE_TAG:-local}"
  AUTO_INCREMENT_TAG="${AUTO_INCREMENT_TAG:-1}"
  UPDATE_ENV_TAG="${UPDATE_ENV_TAG:-1}"
  PUSH_LATEST="${PUSH_LATEST:-1}"
  IROFFER_USER_ID="${IROFFER_USER_ID:-1000}"
  IROFFER_GROUP_ID="${IROFFER_GROUP_ID:-1000}"

  if [ "${AUTO_INCREMENT_TAG}" = "1" ]; then
    IMAGE_TAG="$(increment_image_tag "${IMAGE_TAG}")"
    persist_image_tag_to_env "${IMAGE_TAG}"
    echo "Auto-incremented IMAGE_TAG to ${IMAGE_TAG}"
  fi

  repo="${DOCKERHUB_REPO:-jenga201/iroffer-dinoga}"

  image_ref="${repo}:${IMAGE_TAG}"

  docker_login_if_requested

  echo "Building fresh image: ${image_ref}"
  docker build \
    --pull \
    --no-cache \
    --build-arg IROFFER_USER_ID="${IROFFER_USER_ID}" \
    --build-arg IROFFER_GROUP_ID="${IROFFER_GROUP_ID}" \
    -t "${image_ref}" \
    .

  echo "Pushing ${image_ref}"
  docker push "${image_ref}"

  if [ "${PUSH_LATEST}" = "1" ] && [ "${IMAGE_TAG}" != "latest" ]; then
    latest_ref="${repo}:latest"
    echo "Tagging and pushing ${latest_ref}"
    docker tag "${image_ref}" "${latest_ref}"
    docker push "${latest_ref}"
  fi

  echo "Done: ${image_ref}"
}

main "$@"
