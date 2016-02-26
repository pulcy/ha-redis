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

## Options

- `--announce-ip` - Set the IP address to announce under the master key. Only needed when not using `--docker-url`.
- `--announce-port` - Set the port number to announce under the master key. Only needed when not using `--docker-url`.
- `--container-name` - Name of the docker container running this process.
- `--docker-url` - URL of the docker daemon to fetch IP/port information from.
- `--etcd-url` - The host part is used as ETCD address, the path is used as the master key.
- `--log-level` - Set minimum log level (debug|info|warning|error)
- `--master-ttl` - TTL of the master key in ETCD.
- `--redis-appendonly` - If set, append-only mode will be turned on (can also be done in configuration file).
- `--redis-conf` - Path of a redis configuration file.
