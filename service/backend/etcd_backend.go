package backend

import (
	"time"

	"github.com/coreos/etcd/client"
	retry "github.com/giantswarm/retry-go"
	logging "github.com/op/go-logging"
	"golang.org/x/net/context"
)

// NewEtcdBackend creates a new Backend that uses ETCD.
func NewEtcdBackend(log *logging.Logger, endpoints []string, etcdPath string, masterTTL time.Duration) (Backend, error) {
	cfg := client.Config{
		Transport: client.DefaultTransport,
		Endpoints: endpoints,
	}
	masterKey := etcdPath
	c, err := client.New(cfg)
	if err != nil {
		return nil, maskAny(err)
	}

	go c.AutoSync(context.Background(), time.Second*30)

	return &etcdBackend{
		log:       log,
		client:    c,
		masterKey: masterKey,
		masterTTL: masterTTL,
	}, nil

}

type etcdBackend struct {
	log                 *logging.Logger
	client              client.Client
	masterWatcher       client.Watcher
	recentWatcherErrors int
	masterKey           string
	masterTTL           time.Duration
}

const (
	maxRecentWatcherErrors = 5 // Number of recent errors before the watcher is discarded and rebuild
)

// TryBecomeMaster tries to create a value in ETCD under the master key.
// On success, we're the master, otherwise we're the slave.
func (s *etcdBackend) TryBecomeMaster(ourRedisUrl string) (bool, string, error) {
	kAPI := client.NewKeysAPI(s.client)
	options := &client.SetOptions{
		PrevExist: client.PrevNoExist,
		TTL:       s.masterTTL,
	}
	if _, err := kAPI.Set(context.Background(), s.masterKey, ourRedisUrl, options); isEtcdError(err, client.ErrorCodeNodeExist) {
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

// UpdateMaster tries to update the master key
func (s *etcdBackend) UpdateMaster(ourRedisUrl string) error {
	update := func() error {
		kAPI := client.NewKeysAPI(s.client)
		options := &client.SetOptions{
			PrevValue: ourRedisUrl,
			PrevExist: client.PrevExist,
			TTL:       s.masterTTL,
			//Refresh:   true,
		}
		if _, err := kAPI.Set(context.Background(), s.masterKey, ourRedisUrl, options); err != nil {
			// An error occurred
			return maskAny(err)
		}
		return nil
	}

	if err := retry.Do(update,
		// Make sure we fail asap on key-not-found errors
		retry.RetryChecker(func(err error) bool { return !isEtcdError(err, client.ErrorCodeKeyNotFound) }),
		retry.Timeout(s.masterTTL/3),
		retry.MaxTries(25),
		retry.Sleep(time.Millisecond*250)); err != nil {
		return maskAny(err)
	}

	// Success, we're still the master
	return nil
}

// RemoveMaster tries to remove the master key so another instance can become master
func (s *etcdBackend) RemoveMaster(ourRedisUrl string) error {
	kAPI := client.NewKeysAPI(s.client)
	options := &client.DeleteOptions{
		PrevValue: ourRedisUrl,
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

// WatchForMasterChanges watched ETCD for changes in the master key.
// It will return as soon as a master change was detected.
func (s *etcdBackend) WatchForMasterChanges(masterURL string) error {
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
			s.log.Infof("Change detected, node=nil: %#v", resp)
			return nil
		}
		if resp.Node.Value != masterURL {
			// Change detected
			s.log.Infof("Change in master key detected: '%s' -> '%s'", masterURL, resp.Node.Value)
			return nil
		}
		s.log.Debugf("Etcd watch triggered, no change detected")
	}
}

func isEtcdError(err error, code int) bool {
	if etcdErr, ok := err.(client.Error); ok {
		return etcdErr.Code == code
	}
	return false
}
