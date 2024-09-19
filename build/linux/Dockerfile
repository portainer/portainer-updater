ARG GIT_COMMIT=unspecified
FROM portainer/base:latest

LABEL git_commit=$GIT_COMMIT

COPY dist /

ENTRYPOINT [ "/portainer-updater" ]