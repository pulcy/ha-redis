// Copyright (c) 2017 Pulcy.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/op/go-logging"
	"github.com/spf13/cobra"

	"github.com/pulcy/ha-redis/middleware"
	k8s "github.com/pulcy/ha-redis/pkg/kubernetes"
	"github.com/pulcy/ha-redis/service"
	"github.com/pulcy/ha-redis/service/backend"
	"github.com/pulcy/ha-redis/service/environment"
	"github.com/pulcy/ha-redis/service/proxy"
)

const (
	projectName = "ha-redis"

	defaultEtcdPath = "/master"
)

var (
	projectVersion             = "dev"
	projectBuild               = "dev"
	defaultMasterTTL           = time.Minute
	defaultLogLevel            = "info"
	defaultRedisAppendOnlyPath = "/data/appendonly.aof"
	defaultHost                = "127.0.0.1"
	defaultPort                = 8080
	defaultRedisPort           = 6380
	defaultClientPort          = 6379
	defaultK8sAnnotationKey    = "pulcy.com/haredis.lock"
	defaultHAProxyPath         = "haproxy"
	defaultHAProxyConfPath     = "/data/config/haproxy.conf"
)

var (
	cmdMain = &cobra.Command{
		Use:   projectName,
		Short: "Perform automatic Redis master election",
		Run:   cmdMainRun,
	}
	log   = logging.MustGetLogger(projectName)
	flags struct {
		// ETCD
		etcdURL       string
		etcdEndpoints []string
		etcdPath      string
		// Docker
		dockerURL     string
		containerName string
		// Kubernetes
		k8sNamespace      string
		k8sPodName        string
		k8sReplicaSetName string
		k8sAnnotationKey  string
		// Server
		Host string
		Port int
		// Service
		announceIP   string
		announcePort int
		clientPort   int
		// Proxy
		haproxyPath     string
		haproxyConfPath string
		service.ServiceConfig
		logLevel string
	}
)

func init() {
	logging.SetFormatter(logging.MustStringFormatter("[%{level:-5s}] %{message}"))
	cmdMain.Flags().StringVar(&flags.Host, "host", defaultHost, "IP address to bind to")
	cmdMain.Flags().IntVar(&flags.Port, "port", defaultPort, "Port to listen on")
	cmdMain.Flags().StringVar(&flags.announceIP, "announce-ip", "", "IP address of master to announce when becoming a master")
	cmdMain.Flags().IntVar(&flags.announcePort, "announce-port", defaultRedisPort, "Port of master to announce when becoming a master")
	cmdMain.Flags().IntVar(&flags.clientPort, "client-port", defaultClientPort, "Port that redis clients will listen on")
	cmdMain.Flags().IntVar(&flags.RedisPort, "redis-port", defaultRedisPort, "Port redis will listen on")
	cmdMain.Flags().StringVar(&flags.RedisConf, "redis-conf", "", "Path of redis configuration file")
	cmdMain.Flags().BoolVar(&flags.RedisAppendOnly, "redis-appendonly", false, "If set, will turn on appendonly mode in redis")
	cmdMain.Flags().StringVar(&flags.RedisAppendOnlyPath, "redis-appendonly-path", defaultRedisAppendOnlyPath, "Path of the append-only file which will be checked at startup")
	cmdMain.Flags().StringVar(&flags.etcdURL, "etcd-url", "", "URL of ETCD (path=master key)")
	cmdMain.Flags().StringSliceVar(&flags.etcdEndpoints, "etcd-endpoint", nil, "Etcd client endpoints")
	cmdMain.Flags().StringVar(&flags.etcdPath, "etcd-path", defaultEtcdPath, "Path into etcd namespace")
	cmdMain.Flags().DurationVar(&flags.MasterTTL, "master-ttl", defaultMasterTTL, "TTL of master key in ETCD")
	cmdMain.Flags().StringVar(&flags.dockerURL, "docker-url", "", "URL of docker daemon")
	cmdMain.Flags().StringVar(&flags.containerName, "container-name", "", "Name of the docker container running this process")
	cmdMain.Flags().StringVar(&flags.logLevel, "log-level", defaultLogLevel, "Set minimum log level (debug|info|warning|error)")
	defaultK8sNamespace := k8s.GetNamespace()
	defaultK8sPod := os.Getenv("J2_POD_NAME")
	cmdMain.Flags().StringVar(&flags.k8sNamespace, "kubernetes-namespace", defaultK8sNamespace, "Namespace of pod containing this process")
	cmdMain.Flags().StringVar(&flags.k8sPodName, "kubernetes-pod", defaultK8sPod, "Name of pod containing this process")
	cmdMain.Flags().StringVar(&flags.k8sReplicaSetName, "kubernetes-replicaset", "", "Name of replicaSet to use as lock resource (if not set, fetched via pod)")
	cmdMain.Flags().StringVar(&flags.k8sAnnotationKey, "kubernetes-annotation-key", defaultK8sAnnotationKey, "Name of Kubernetes annotation used for locking")
	// Proxy
	cmdMain.Flags().StringVar(&flags.haproxyPath, "haproxy-path", defaultHAProxyPath, "Full path of haproxy binary")
	cmdMain.Flags().StringVar(&flags.haproxyConfPath, "haproxy-conf-path", defaultHAProxyConfPath, "Full path of haproxy config file")
}

func main() {
	cmdMain.Execute()

}

func cmdMainRun(cmd *cobra.Command, args []string) {
	// Check flags
	if flags.etcdURL != "" {
		etcdUrl, err := url.Parse(flags.etcdURL)
		if err != nil {
			Exitf("--etcd-url '%s' is not valid: %#v", flags.etcdURL, err)
		}
		flags.etcdEndpoints = []string{fmt.Sprintf("%s://%s", etcdUrl.Scheme, etcdUrl.Host)}
		flags.etcdPath = etcdUrl.Path
	}

	useKubernetes := false
	if k8s.IsRunningInKubernetes() {
		useKubernetes = true
	}

	if useKubernetes {
		if flags.announceIP == "" && flags.k8sPodName != "" {
			var err error
			flags.announceIP, err = k8s.GetPodIP(flags.k8sNamespace, flags.k8sPodName)
			if err != nil {
				Exitf("Failed to get pod IP: %#v", err)
			}
		}
		assertArgIsSet(flags.announceIP, "--announce-ip")
	} else {
		if flags.dockerURL != "" || flags.containerName != "" {
			if flags.dockerURL == "" {
				Exitf("--docker-url is missing")
			}
			if flags.containerName == "" {
				Exitf("--container-name is missing")
			}
		} else {
			assertArgIsSet(flags.announceIP, "--announce-ip")
		}
	}

	// Set log level
	level, err := logging.LogLevel(flags.logLevel)
	if err != nil {
		Exitf("Invalid log-level '%s': %#v", flags.logLevel, err)
	}
	logging.SetLevel(level, projectName)

	// Create Environment
	var env environment.Environment
	if useKubernetes {
		env, err = environment.NewKubernetesEnvironment(flags.announceIP, flags.announcePort)
		if err != nil {
			Exitf("Failed to create Kubernetes environment: %#v\n", err)
		}
	} else {
		env, err = environment.NewDockerEnvironment(log, flags.RedisPort, flags.announceIP, flags.announcePort, flags.dockerURL, flags.containerName)
		if err != nil {
			Exitf("Failed to create Docker environment: %#v\n", err)
		}
	}

	// Create backend
	var b backend.Backend
	if useKubernetes {
		if flags.k8sReplicaSetName == "" && flags.k8sPodName != "" {
			flags.k8sReplicaSetName, err = k8s.GetCreatingReplicaSetName(flags.k8sNamespace, flags.k8sPodName)
			if err != nil {
				Exitf("Failed to find creating ReplicaSet: %#v\n", err)
			}
		}
		ourMasterURL := fmt.Sprintf("%s:%d", flags.announceIP, flags.announcePort)
		b, err = backend.NewKubernetesBackend(log, flags.k8sNamespace, flags.k8sReplicaSetName, flags.k8sAnnotationKey, ourMasterURL, flags.MasterTTL)
		if err != nil {
			Exitf("Failed to create Kubernetes backend: %#v\n", err)
		}
	} else {
		b, err = backend.NewEtcdBackend(log, flags.etcdEndpoints, flags.etcdPath, flags.MasterTTL)
		if err != nil {
			Exitf("Failed to create ETCD backend: %#v\n", err)
		}
	}

	// Create proxy
	p, err := proxy.NewHAProxy(log, flags.clientPort, flags.haproxyPath, flags.haproxyConfPath)
	if err != nil {
		Exitf("Failed to create HA proxy: %#v\n", err)
	}

	// Create service
	s, err := service.NewService(flags.ServiceConfig, service.ServiceDependencies{
		Logger:      log,
		Backend:     b,
		Environment: env,
		Proxy:       p,
	})
	if err != nil {
		Exitf("Failed to create service: %#v\n", err)
	}

	// Run middleware server
	middleware.RunServer(flags.Host, strconv.Itoa(flags.Port))

	if err := s.Run(); err != nil {
		Exitf("%s failed: %#v\n", projectName, err)
	}
}

func UsageFunc(cmd *cobra.Command, args []string) {
	cmd.Usage()
}

func Exitf(format string, args ...interface{}) {
	if !strings.HasSuffix(format, "\n") {
		format = format + "\n"
	}
	fmt.Printf(format, args...)
	os.Exit(1)
}

func def(envKey, defaultValue string) string {
	s := os.Getenv(envKey)
	if s == "" {
		s = defaultValue
	}
	return s
}

func assertArgIsSet(arg, argKey string) {
	if arg == "" {
		Exitf("%s must be set\n", argKey)
	}
}
