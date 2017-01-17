package environment

// NewKubernetesEnvironment creates a new Environment that fetches IP&address from docker.
func NewKubernetesEnvironment(defaultAnnounceIP string, defaultAnnouncePort int) (Environment, error) {
	return &k8sEnvironment{
		defaultAnnounceIP:   defaultAnnounceIP,
		defaultAnnouncePort: defaultAnnouncePort,
	}, nil
}

type k8sEnvironment struct {
	defaultAnnounceIP   string
	defaultAnnouncePort int
}

// fetchAnnounceInfoFromContainer connects to docker to ask for the announce IP
// and port for the configured container name.
func (s *k8sEnvironment) FetchAnnounceInfo() (announceIP string, announcePort int, err error) {
	return s.defaultAnnounceIP, s.defaultAnnouncePort, nil
}
