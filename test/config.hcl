job "haredis_test" {
    group "queue" {
        count = 2
        task "redis" {
            image = "pulcy/ha-redis:20170117115000"
            ports = ["${private_ipv4}::6379", 8080]
            args = [
                "--host=0.0.0.0",
                "--port=8080",
                "--etcd-endpoint=${etcd_endpoints}",
                "--etcd-path=/pulcy/service/${job}-${group}-master/the:master:6379",
                "--container-name=${container}",
                "--docker-url=unix:///var/run/docker.sock",
                "--redis-appendonly"
            ]
            network = "default"
            volumes = [
                "/var/run/docker.sock:/var/run/docker.sock",
            ]
        }
    }
}