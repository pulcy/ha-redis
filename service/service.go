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

package service

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/op/go-logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/pulcy/ha-redis/service/backend"
)

const (
	namespace = "ha_redis"
)

var (
	currentMode = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "current_mode",
		Help:      "Current operating mode (1=master, 0=slave).",
	})
	masterAttempts = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "act_as_master_attempts",
		Help:      "Number of times actAsMaster is called.",
	})
	slaveAttempts = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "act_as_slave_attempts",
		Help:      "Number of times actAsSave is called.",
	})
	updateMasterErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "update_master_errors",
		Help:      "Number of updateMaster errors.",
	})
	watchForMasterChangesErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "watch_for_master_changes_errors",
		Help:      "Number of watchForMasterChanges errors.",
	})
)

func init() {
	prometheus.MustRegister(currentMode)
	prometheus.MustRegister(masterAttempts)
	prometheus.MustRegister(slaveAttempts)
	prometheus.MustRegister(updateMasterErrors)
	prometheus.MustRegister(watchForMasterChangesErrors)
}

type ServiceConfig struct {
	MasterTTL time.Duration

	AnnounceIP    string
	AnnouncePort  int
	DockerURL     string
	ContainerName string

	RedisConf           string
	RedisAppendOnly     bool
	RedisAppendOnlyPath string
}

type ServiceDependencies struct {
	Logger  *logging.Logger
	Backend backend.Backend
}

type Service struct {
	ServiceConfig
	ServiceDependencies

	ourRedisUrl string
}

// NewService initializes a new Service.
func NewService(config ServiceConfig, deps ServiceDependencies) (*Service, error) {
	return &Service{
		ServiceConfig:       config,
		ServiceDependencies: deps,
	}, nil
}

// Run performs the actual service.
func (s *Service) Run() error {
	// Check appendonly file
	if err := s.checkRedisAppendOnlyFile(); err != nil {
		return maskAny(err)
	}

	// Fetch announce info
	if err := s.fetchAnnounceInfoFromContainer(); err != nil {
		return maskAny(err)
	}
	// Format the redis URL to use if we're master
	s.ourRedisUrl = fmt.Sprintf("%s:%d", s.AnnounceIP, s.AnnouncePort)

	// Start our local redis
	exitChan := make(chan int)
	redisCmd, err := s.startRedis(exitChan)
	if err != nil {
		return maskAny(err)
	}

	// Upon redis exit, exit this process also
	go func() {
		exitCode := <-exitChan
		os.Exit(exitCode)
	}()

	// Listen for termination signals and forward them to redis
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

		// Wait for signal
		<-c

		// Attempt to remove ourself as master (if we're slave this will silently fail)
		if err := s.Backend.RemoveMaster(s.ourRedisUrl); err != nil {
			s.Logger.Errorf("Remove master cleanup failed: %#v", err)
		}

		// Send termination to redis
		s.Logger.Info("Sending TERM signal to redis")
		redisCmd.Process.Signal(syscall.SIGTERM)
	}()

	for {
		if success, masterURL, err := s.Backend.TryBecomeMaster(s.ourRedisUrl); err != nil {
			s.Logger.Errorf("Error in tryBecomeMaster, retry soon: %#v", err)
			time.Sleep(time.Second * 5)
		} else if success {
			// We're now the master
			if err := s.actAsMaster(); err != nil {
				s.Logger.Errorf("actAsMaster failed, retry soon: %#v", err)
				time.Sleep(time.Second * 3)
			}
		} else {
			// We're now slave
			if err := s.actAsSlave(masterURL); err != nil {
				s.Logger.Errorf("actAsSlave failed, retry soon: %#v", err)
				time.Sleep(time.Second * 2)
			}
		}
	}
}

func (s *Service) actAsMaster() error {
	masterAttempts.Inc()

	// Configure redis as master
	if err := s.configureSlaveOfNoOne(); err != nil {
		s.Logger.Errorf("Error in configureSlaveOfNoOne: %#v", err)
		return maskAny(err)
	}
	s.Logger.Infof("Acting as master on '%s'", s.ourRedisUrl)
	currentMode.Set(1) // Master

	// Update our master key in backend
	for {
		if err := s.Backend.UpdateMaster(s.ourRedisUrl); err != nil {
			s.Logger.Errorf("Error in updateMaster: %#v", err)
			updateMasterErrors.Inc()
			return maskAny(err)
		}
		time.Sleep(s.MasterTTL / 2)
	}
}

func (s *Service) actAsSlave(masterURL string) error {
	slaveAttempts.Inc()

	// Configure redis as slave
	masterIP, masterPort, err := net.SplitHostPort(masterURL)
	if err != nil {
		return maskAny(err)
	}
	if err := s.configureSlaveOf(masterIP, masterPort); err != nil {
		s.Logger.Errorf("Error in configureSlaveOf: %#v", err)
		return maskAny(err)
	}
	s.Logger.Infof("Acting as slave of '%s'", masterURL)
	currentMode.Set(0) // Slave

	for {
		// Wait for changes in backend
		if err := s.Backend.WatchForMasterChanges(masterURL); err == nil {
			// Different master
			return nil
		} else {
			watchForMasterChangesErrors.Inc()
			s.Logger.Infof("watchForMasterChanges failed: %#v", err)
		}

		if err := s.ping(masterIP, masterPort); err != nil {
			// Can not ping the master, assume it is gone
			s.Logger.Infof("cannot ping master, assume it is gone: %v", err)
			return nil
		}

		// Ping succeeds, just retry
		s.Logger.Info("ping to master still succeeds, remaining slave for now")
		time.Sleep(time.Millisecond * 250)
	}
}
