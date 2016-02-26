# HA-Redis

HA-Redis is a wrapper around redis that uses ETCD to provide:

- Automatic master election.
- Registers master as service into ETCD in the same format as [registrator](https://github.com/gliderlabs/registrator).
- Automatic querying of host IP:port from docker.

## Usage

```
docker run -d \
    -p ${COREOS_PRIVATE_IPV4}::6379 \
    -v /var/run/docker.sock:/var/run/docker.sock \
    --name redis \
    pulcy/ha-redis:latest \
    --etcd-url http://${COREOS_PRIVATE_IPV4}:4001/pulcy/service/ha-redis/master \
    --container-name redis  \
    --docker-url unix:///var/run/docker.sock
```
