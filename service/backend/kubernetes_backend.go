package backend

import (
	"fmt"
	"time"

	kc "github.com/YakLabs/k8s-client/http"
	retry "github.com/giantswarm/retry-go"
	logging "github.com/op/go-logging"
	lock "github.com/pulcy/kube-lock"
	k8s "github.com/pulcy/kube-lock/k8s"
)

// NewKubernetesBackend creates a new Backend that uses Kubernetes API.
func NewKubernetesBackend(log *logging.Logger, namespace, replicaSetName, annotationKey, ourMasterURL string, masterTTL time.Duration) (Backend, error) {
	c, err := kc.NewInCluster()
	if err != nil {
		return nil, maskAny(err)
	}
	l, err := k8s.NewReplicaSetLock(namespace, replicaSetName, c, annotationKey, ourMasterURL, masterTTL)
	if err != nil {
		return nil, maskAny(err)
	}

	return &k8sBackend{
		log:       log,
		lock:      l,
		masterTTL: masterTTL,
	}, nil

}

type k8sBackend struct {
	log       *logging.Logger
	lock      lock.KubeLock
	masterTTL time.Duration
}

// TryBecomeMaster tries to create a value in Kubernetes.
// On success, we're the master, otherwise we're the slave.
func (s *k8sBackend) TryBecomeMaster(ourRedisUrl string) (bool, string, error) {
	success := false
	currentMaster := ""
	acquire := func() error {
		err := s.lock.Acquire()
		if err == nil {
			success = true
			return nil
		}
		if !lock.IsAlreadyLocked(err) {
			// Unknown error
			return maskAny(err)
		}
		// Already locked, try fetching current owner
		owner, err := s.lock.CurrentOwner()
		if err != nil {
			return maskAny(err)
		}
		if owner != "" {
			// We found the current owner
			currentMaster = owner
			return nil
		}
		return maskAny(fmt.Errorf("No owner found, retry..."))
	}

	if err := retry.Do(acquire,
		retry.Timeout(s.masterTTL/3),
		retry.MaxTries(25),
		retry.Sleep(time.Millisecond*250)); err != nil {
		return false, "", maskAny(err)
	}

	return success, currentMaster, nil
}

// UpdateMaster tries to update the master key
func (s *k8sBackend) UpdateMaster(ourRedisUrl string) error {
	update := func() error {
		if err := s.lock.Acquire(); err != nil {
			return maskAny(err)
		}
		return nil
	}

	if err := retry.Do(update,
		// Make sure we fail asap on key-not-found errors
		retry.Timeout(s.masterTTL/3),
		retry.MaxTries(25),
		retry.Sleep(time.Millisecond*250)); err != nil {
		return maskAny(err)
	}

	// Success, we're still the master
	return nil
}

// RemoveMaster tries to remove the master key so another instance can become master
func (s *k8sBackend) RemoveMaster(ourRedisUrl string) error {
	err := s.lock.Release()
	if lock.IsNotLockedByMe(err) {
		// Current master key differs, no need for us to cleanup
		return nil
	} else if err != nil {
		// An error occurred
		return maskAny(err)
	}

	return nil
}

// WatchForMasterChanges watched Kubernetes for changes in the master key.
// It will return as soon as a master change was detected.
func (s *k8sBackend) WatchForMasterChanges(masterURL string) error {
	for {
		owner, err := s.lock.CurrentOwner()
		if err == nil {
			if owner != masterURL {
				// Change detected
				s.log.Infof("Change in master detected: '%s' -> '%s'", masterURL, owner)
				return nil
			}
		} else if err != nil {
			s.log.Debugf("CurrentOwner failed: %#v", err)
		}
		time.Sleep(time.Second)
	}
}
