ARG GIT_COMMIT=unspecified
ARG OSVERSION
FROM --platform=linux/amd64 gcr.io/k8s-staging-e2e-test-images/windows-servercore-cache:1.0-linux-amd64-${OSVERSION} AS core
FROM --platform=linux/amd64 alpine:3.13.0 AS downloader

FROM mcr.microsoft.com/windows/nanoserver:${OSVERSION}

COPY --from=core /Windows/System32/netapi32.dll /Windows/System32/netapi32.dll

USER ContainerAdministrator

LABEL git_commit=$GIT_COMMIT

COPY dist /app/
ENTRYPOINT ["C:/app/portainer-updater.exe"]
