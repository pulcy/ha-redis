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
	"time"

	"github.com/coreos/etcd/client"
	"github.com/giantswarm/retry-go"
	"golang.org/x/net/context"
)

const (
	maxRecentWatcherErrors = 5 // Number of recent errors before the watcher is discarded and rebuild
)

// tryBecomeMaster tries to create a value in ETCD under the master key.
// On success, we're the master, otherwise we're the slave.
func (s *Service) tryBecomeMaster() (bool, string, error) {
	kAPI := client.NewKeysAPI(s.client)
	options := &client.SetOptions{
		PrevExist: client.PrevNoExist,
		TTL:       s.MasterTTL,
	}
	if _, err := kAPI.Set(context.Background(), s.masterKey, s.ourRedisUrl, options); isEtcdError(err, client.ErrorCodeNodeExist) {
		// Node already exists, we're not the master
		// Try to read the current master URL and return it
		resp, err := kAPI.Get(context.Background(), s.masterKey, nil)
		if err != nil {
			return false, "", maskAny(err)
		}
		return false, resp.Node.Value, nil
	} else if err != nil {
		// Another error occurred
		return false, "", maskAny(err)
	}

	// Success, we're the master
	return true, "", nil
}

// updateMaster tries to update the master key
func (s *Service) updateMaster() error {
	update := func() error {
		kAPI := client.NewKeysAPI(s.client)
		options := &client.SetOptions{
			PrevValue: s.ourRedisUrl,
			PrevExist: client.PrevExist,
			TTL:       s.MasterTTL,
			//Refresh:   true,
		}
		if _, err := kAPI.Set(context.Background(), s.masterKey, s.ourRedisUrl, options); err != nil {
			// An error occurred
			return maskAny(err)
		}
		return nil
	}

	if err := retry.Do(update,
		// Make sure we fail asap on key-not-found errors
		retry.RetryChecker(func(err error) bool { return !isEtcdError(err, client.ErrorCodeKeyNotFound) }),
		retry.Timeout(s.MasterTTL/3),
		retry.MaxTries(25),
		retry.Sleep(time.Millisecond*250)); err != nil {
		return maskAny(err)
	}

	// Success, we're still the master
	return nil
}

// removeMaster tries to remove the master key so another instance can become master
func (s *Service) removeMaster() error {
	kAPI := client.NewKeysAPI(s.client)
	options := &client.DeleteOptions{
		PrevValue: s.ourRedisUrl,
		Recursive: false,
		Dir:       false,
	}
	if _, err := kAPI.Delete(context.Background(), s.masterKey, options); isEtcdError(err, client.ErrorCodeTestFailed) {
		// Current master key differs, no need for us to cleanup
		return nil
	} else if isEtcdError(err, client.ErrorCodeKeyNotFound) {
		// No master key found, no need for us to cleanup
		return nil
	} else if err != nil {
		// An error occurred
		return maskAny(err)
	}

	return nil
}

// watchForMasterChanges watched ETCD for changes in the master key.
// It will return as soon as a master change was detected.
func (s *Service) watchForMasterChanges(masterURL string) error {
	for {
		if s.masterWatcher == nil || s.recentWatcherErrors > maxRecentWatcherErrors {
			kAPI := client.NewKeysAPI(s.client)
			options := &client.WatcherOptions{
				Recursive: false,
			}
			s.recentWatcherErrors = 0
			s.masterWatcher = kAPI.Watcher(s.masterKey, options)
		}
		resp, err := s.masterWatcher.Next(context.Background())
		if err != nil {
			s.recentWatcherErrors++
			return maskAny(err)
		}
		s.recentWatcherErrors = 0
		if resp.Node == nil {
			s.Logger.Infof("Change detected, node=nil: %#v", resp)
			return nil
		}
		if resp.Node.Value != masterURL {
			// Change detected
			s.Logger.Infof("Change in master key detected: '%s' -> '%s'", masterURL, resp.Node.Value)
			return nil
		}
		s.Logger.Debugf("Etcd watch triggered, no change detected")
	}
}

func isEtcdError(err error, code int) bool {
	if etcdErr, ok := err.(client.Error); ok {
		return etcdErr.Code == code
	}
	return false
}
