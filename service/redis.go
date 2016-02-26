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
	"bytes"
	"os"
	"os/exec"
	"time"

	"github.com/adjust/redis"
	"github.com/giantswarm/retry-go"
)

// Start the redis process
func (s *Service) startRedis(exitChan chan int) (*exec.Cmd, error) {
	args := []string{}
	if s.RedisAppendOnly {
		args = append(args, "--appendonly", "yes")
	}
	if s.RedisConf != "" {
		args = append(args, s.RedisConf)
	}
	cmd := exec.Command("redis-server", args...)
	cmd.Stdin = bytes.NewReader([]byte{})
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, maskAny(err)
	}

	go func() {
		err := cmd.Wait()
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.Success() {
				s.Logger.Debug("redis-server terminated")
				exitChan <- 0
			} else {
				s.Logger.Errorf("redis-server terminated with error: %#v", err)
				exitChan <- 1
			}
		} else {
			s.Logger.Errorf("redis-server terminated with unknown error: %#v", err)
			exitChan <- 2
		}
	}()

	return cmd, nil
}

// configureSlaveOf configures our redis instance as slave of the given master.
func (s *Service) configureSlaveOf(masterIP, masterPort string) error {
	client, err := s.connectRedis()
	if err != nil {
		return maskAny(err)
	}
	if err := client.SlaveOf(masterIP, masterPort).Err(); err != nil {
		return maskAny(err)
	}
	return nil
}

// configureSlaveOfNoOne configures our redis instance as not replicating anything.
func (s *Service) configureSlaveOfNoOne() error {
	client, err := s.connectRedis()
	if err != nil {
		return maskAny(err)
	}
	if err := client.SlaveOf("NO", "ONE").Err(); err != nil {
		return maskAny(err)
	}
	return nil
}

// connectRedis creates a client connection to our own redis server.
func (s *Service) connectRedis() (*redis.Client, error) {
	options := &redis.Options{
		Addr:        "127.0.0.1:6379",
		IdleTimeout: 240 * time.Second,
	}
	client := redis.NewTCPClient(options)

	if err := retry.Do(func() error {
		if err := client.Ping().Err(); err != nil {
			s.Logger.Debug(nil, "Failed to ping redis: %#v", err)
			return maskAny(err)
		}
		return nil
	},
		retry.Timeout(time.Second*30),
		retry.MaxTries(5),
		retry.Sleep(time.Second*2)); err != nil {
		return nil, maskAny(err)
	}

	return client, nil
}
