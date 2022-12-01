ARG GIT_COMMIT=unspecified

FROM alpine:3.6 as base
RUN apk add -U --no-cache ca-certificates


FROM scratch

LABEL git_commit=$GIT_COMMIT

COPY --from=base /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY dist /

ENTRYPOINT [ "/portainer-updater" ]