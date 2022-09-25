#!/bin/bash
set -euo pipefail

DEBUG=${DEBUG:-""}
if [[ -n $DEBUG ]]; then
  set +x
fi

BASE_REPO=${1:-"portainer"}
REPO=${REPO:-"$BASE_REPO/portainer-updater"}
TAG=${2:-"latest"}

BUILDS=('linux/amd64' 'linux/arm64' 'linux/arm' 'windows/amd64')


docker_image_build_and_push()
{
  os=${1}
  arch=${2}
  repo=${3}
  tag=${4}

  image="${repo}:${tag}-${os}-${arch}"
  echo "Building $image"

  dockerfile="build/linux/Dockerfile"
  build_args=()
  if [[ ${os} == "windows" ]]; then
      dockerfile="build/windows/Dockerfile"
      build_args+=(--build-arg OSVERSION=1809)
  fi

  docker buildx build --push -f ${dockerfile} "${build_args[@]}" --platform "${os}/${arch}" --tag "${repo}:${tag}-${os}-${arch}" .
}

docker_manifest_create_and_push()
{
  repo=${1}
  tag=${2}

  echo "building manifest $repo:$tag"

  for build in "${BUILDS[@]}"
  do
    IFS='/' read -ra build_parts <<< "$build"
    os=${build_parts[0]}
    arch=${build_parts[1]}

    image="${repo}:${tag}-${os}-${arch}"
    
    echo docker manifest create --amend "${repo}:${tag} $image"
    docker manifest create --amend "${repo}:${tag} $image"
    echo docker manifest annotate "${repo}:${tag} ${image}" --os "${os}" --arch "${arch}"
    docker manifest annotate "${repo}:${tag} ${image}" --os "${os}" --arch "${arch}"
  done  
  
  docker manifest push "${repo}:${tag}"
}


for build in "${BUILDS[@]}"
do
  echo "Creating build $build ..."
  IFS='/' read -ra build_parts <<< "$build"
  os=${build_parts[0]}
  arch=${build_parts[1]}

  make clean
  make PLATFORM="${os}" ARCH="${arch}" release
  docker_image_build_and_push "${os}" "${arch}" "${REPO}" "${TAG}" 
done

docker_manifest_create_and_push "${REPO}" "${TAG}"