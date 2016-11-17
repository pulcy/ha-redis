// Copyright (c) 2016 Pulcy.
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
	"github.com/pulcy/ha-redis/service"
)

const (
	projectName = "ha-redis"

	defaultEtcdURL = "http://localhost:4001/master"
)

var (
	projectVersion             = "dev"
	projectBuild               = "dev"
	defaultMasterTTL           = time.Minute
	defaultLogLevel            = "info"
	defaultRedisAppendOnlyPath = "/data/appendonly.aof"
	defaultHost                = "127.0.0.1"
	defaultPort                = 8080
)

var (
	cmdMain = &cobra.Command{
		Use:   projectName,
		Short: "Perform automatic Redis master election",
		Run:   cmdMainRun,
	}
	log   = logging.MustGetLogger(projectName)
	flags struct {
		etcdURL string
		Host    string
		Port    int
		service.ServiceConfig
		logLevel string
	}
)

func init() {
	logging.SetFormatter(logging.MustStringFormatter("[%{level:-5s}] %{message}"))
	cmdMain.Flags().StringVar(&flags.Host, "host", defaultHost, "IP address to bind to")
	cmdMain.Flags().IntVar(&flags.Port, "port", defaultPort, "Port to listen on")
	cmdMain.Flags().StringVar(&flags.AnnounceIP, "announce-ip", "", "IP address of master to announce when becoming a master")
	cmdMain.Flags().IntVar(&flags.AnnouncePort, "announce-port", 6379, "Port of master to announce when becoming a master")
	cmdMain.Flags().StringVar(&flags.RedisConf, "redis-conf", "", "Path of redis configuration file")
	cmdMain.Flags().BoolVar(&flags.RedisAppendOnly, "redis-appendonly", false, "If set, will turn on appendonly mode in redis")
	cmdMain.Flags().StringVar(&flags.RedisAppendOnlyPath, "redis-appendonly-path", defaultRedisAppendOnlyPath, "Path of the append-only file which will be checked at startup")
	cmdMain.Flags().StringVar(&flags.etcdURL, "etcd-url", defaultEtcdURL, "URL of ETCD (path=master key)")
	cmdMain.Flags().StringSliceVar(&flags.EtcdEndpoints, "etcd-endpoint", nil, "Etcd client endpoints")
	cmdMain.Flags().StringVar(&flags.EtcdPath, "etcd-path", "", "Path into etcd namespace")
	cmdMain.Flags().DurationVar(&flags.MasterTTL, "master-ttl", defaultMasterTTL, "TTL of master key in ETCD")
	cmdMain.Flags().StringVar(&flags.DockerURL, "docker-url", "", "URL of docker daemon")
	cmdMain.Flags().StringVar(&flags.ContainerName, "container-name", "", "Name of the docker container running this process")
	cmdMain.Flags().StringVar(&flags.logLevel, "log-level", defaultLogLevel, "Set minimum log level (debug|info|warning|error)")
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
		flags.EtcdEndpoints = []string{fmt.Sprintf("%s://%s", etcdUrl.Scheme, etcdUrl.Host)}
		flags.EtcdPath = etcdUrl.Path
	}

	if flags.DockerURL != "" || flags.ContainerName != "" {
		if flags.DockerURL == "" {
			Exitf("--docker-url is missing")
		}
		if flags.ContainerName == "" {
			Exitf("--container-name is missing")
		}
	} else {
		assertArgIsSet(flags.AnnounceIP, "--announce-ip")
	}

	// Set log level
	level, err := logging.LogLevel(flags.logLevel)
	if err != nil {
		Exitf("Invalid log-level '%s': %#v", flags.logLevel, err)
	}
	logging.SetLevel(level, projectName)

	// Create service
	s, err := service.NewService(flags.ServiceConfig, service.ServiceDependencies{
		Logger: log,
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
