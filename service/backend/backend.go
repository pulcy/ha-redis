package backend

type Backend interface {
	// TryBecomeMaster tries to create a value in ETCD under the master key.
	// On success, we're the master, otherwise we're the slave.
	TryBecomeMaster(ourRedisUrl string) (bool, string, error)

	// UpdateMaster tries to update the master key
	UpdateMaster(ourRedisUrl string) error

	// RemoveMaster tries to remove the master key so another instance can become master
	RemoveMaster(ourRedisUrl string) error

	// WatchForMasterChanges watched ETCD for changes in the master key.
	// It will return as soon as a master change was detected.
	WatchForMasterChanges(masterURL string) error
}
