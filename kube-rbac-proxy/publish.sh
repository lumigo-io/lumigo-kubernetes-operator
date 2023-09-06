#!/usr/bin/env bash
####################################################################################################################
# NOTE: The changes I made to the original publish script are:
# 1. I changed the QUAY_PATH to be our docker repo.
# 2. I changed the VERSION to be the version of the operator (and deleted the part that use to evaluate the version.
####################################################################################################################
# exit immediately when a command fails
set -e
# only exit with zero if all commands of the pipeline exit successfully
set -o pipefail
# error on unset variables
set -u
# for debugging
set -x

QUAY_PATH="${DOCKER_REPO:-quay.io/brancz/kube-rbac-proxy}"
CPU_ARCHS="amd64 arm64 arm ppc64le s390x"
VERSION="${VERSION}"

# build and push arch specific images
for arch in ${CPU_ARCHS}; do
  VERSION="${VERSION}" DOCKER_REPO="${QUAY_PATH}" GOARCH="${arch}" make container
  docker push "${QUAY_PATH}:${VERSION}-${arch}"
done

# Create manifest to join all images under one virtual tag
MANIFEST="docker manifest create -a ${QUAY_PATH}:${VERSION}"
for arch in ${CPU_ARCHS}; do
  MANIFEST="${MANIFEST} ${QUAY_PATH}:${VERSION}-${arch}"
done
eval "${MANIFEST}"

# Annotate to set which image is build for which CPU architecture
for arch in ${CPU_ARCHS}; do
  docker manifest annotate --arch "${arch}" "${QUAY_PATH}:${VERSION}" "${QUAY_PATH}:${VERSION}-${arch}"
done
docker manifest push "${QUAY_PATH}:${VERSION}"
