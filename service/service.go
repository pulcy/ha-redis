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

package service

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/coreos/etcd/client"
	"github.com/op/go-logging"
)

type ServiceConfig struct {
	EtcdURL   string
	MasterTTL time.Duration

	AnnounceIP    string
	AnnouncePort  int
	DockerURL     string
	ContainerName string

	RedisConf       string
	RedisAppendOnly bool
}

type ServiceDependencies struct {
	Logger *logging.Logger
}

type Service struct {
	ServiceConfig
	ServiceDependencies

	client      client.Client
	watcher     client.Watcher
	masterKey   string
	ourRedisUrl string
}

// NewService initializes a new Service.
func NewService(config ServiceConfig, deps ServiceDependencies) (*Service, error) {
	cfg := client.Config{
		Transport: client.DefaultTransport,
	}
	uri, err := url.Parse(config.EtcdURL)
	if err != nil {
		return nil, maskAny(err)
	}
	masterKey := uri.Path
	if uri.Host != "" {
		cfg.Endpoints = append(cfg.Endpoints, "http://"+uri.Host)
	}
	c, err := client.New(cfg)
	if err != nil {
		return nil, maskAny(err)
	}
	kAPI := client.NewKeysAPI(c)
	options := &client.WatcherOptions{
		Recursive: false,
	}
	watcher := kAPI.Watcher(masterKey, options)

	return &Service{
		ServiceConfig:       config,
		ServiceDependencies: deps,

		client:    c,
		watcher:   watcher,
		masterKey: masterKey,
	}, nil
}

// Run performs the actual service.
func (s *Service) Run() error {
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
		if err := s.removeMaster(); err != nil {
			s.Logger.Errorf("Remove master cleanup failed: %#v", err)
		}

		// Send termination to redis
		s.Logger.Info("Sending TERM signal to redis")
		redisCmd.Process.Signal(syscall.SIGTERM)
	}()

	for {
		if success, masterURL, err := s.tryBecomeMaster(); err != nil {
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
	// Configure redis as master
	if err := s.configureSlaveOfNoOne(); err != nil {
		s.Logger.Errorf("Error in configureSlaveOfNoOne: %#v", err)
		return maskAny(err)
	}
	s.Logger.Infof("Acting as master on '%s'", s.ourRedisUrl)

	// Update our master key in ETCD
	for {
		if err := s.updateMaster(); err != nil {
			s.Logger.Errorf("Error in updateMaster: %#v", err)
			return maskAny(err)
		}
		time.Sleep(s.MasterTTL / 2)
	}
}

func (s *Service) actAsSlave(masterURL string) error {
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

	// Wait for changes in ETCD
	if err := s.watchForMasterChanges(masterURL); err != nil {
		return maskAny(err)
	}

	return nil
}
