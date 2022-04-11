# Portainer-upgrader

A tool to upgrade the Portainer software.

## Agent update

```
# Via container name
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock portainer/portainer-updater agent-update portainer_agent 2.12.2

# Via container ID
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock portainer/portainer-updater agent-update e9b3e57700ad 2.12.2
```