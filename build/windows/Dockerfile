ARG GIT_COMMIT=unspecified
ARG OSVERSION
FROM --platform=linux/amd64 gcr.io/k8s-staging-e2e-test-images/windows-servercore-cache:1.0-linux-amd64-${OSVERSION} as core
FROM --platform=linux/amd64 alpine:3.13.0 as downloader
ENV GIT_VERSION 2.44.0
ENV GIT_PATCH_VERSION 1

RUN mkdir mingit/ \
  && wget https://github.com/git-for-windows/git/releases/download/v$GIT_VERSION.windows.$GIT_PATCH_VERSION/MinGit-$GIT_VERSION-busybox-64-bit.zip \
  && unzip MinGit-$GIT_VERSION-busybox-64-bit.zip -d mingit/

FROM mcr.microsoft.com/windows/nanoserver:${OSVERSION}
ENV PATH "C:\mingit\cmd;C:\app;C:\Windows\system32;C:\Windows;"

COPY --from=downloader /mingit mingit/
COPY --from=core /Windows/System32/netapi32.dll /Windows/System32/netapi32.dll

USER ContainerAdministrator

LABEL git_commit=$GIT_COMMIT

COPY dist /app/
ENTRYPOINT ["C:/app/portainer-updater.exe"]
