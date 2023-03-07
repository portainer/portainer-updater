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
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock portainer/portainer-updater:latest {agent/portainer} {schedule_id} {target_image_version} 
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock portainer/portainer-updater:latest agent 1 portainer/portainer-updater:2.18.1 


# Private registry
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock registry.example.com/portainer-updater:latest agent 1 registry.example.com/agent:2.18.1 

# Via container ID
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock portainer/portainer-updater agent-update e9b3e57700ad 2.12.2
```