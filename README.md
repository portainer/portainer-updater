# Portainer-upgrader

A tool to upgrade the Portainer software.

Build:

```
# To just compile it and get the binary generated in dist/
make build

# To compile and build a Docker image:
make image
```

## Agent update

```
# Via container name
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock portainer/portainer-updater agent-update portainer_agent 2.12.2

# Via container ID
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock portainer/portainer-updater agent-update e9b3e57700ad 2.12.2
```