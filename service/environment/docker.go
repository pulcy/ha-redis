package environment

import (
	"fmt"
	"strconv"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	retry "github.com/giantswarm/retry-go"
	logging "github.com/op/go-logging"
)

// NewDockerEnvironment creates a new Environment that fetches IP&address from docker.
func NewDockerEnvironment(log *logging.Logger, defaultAnnounceIP string, defaultAnnouncePort int, dockerURL, containerName string) (Environment, error) {
	return &dockerEnvironment{
		defaultAnnounceIP:   defaultAnnounceIP,
		defaultAnnouncePort: defaultAnnouncePort,
		log:                 log,
		dockerURL:           dockerURL,
		containerName:       containerName,
	}, nil
}

type dockerEnvironment struct {
	defaultAnnounceIP   string
	defaultAnnouncePort int
	log                 *logging.Logger
	dockerURL           string
	containerName       string
}

const (
	redisPort = "6379/tcp"
)

// fetchAnnounceInfoFromContainer connects to docker to ask for the announce IP
// and port for the configured container name.
func (s *dockerEnvironment) FetchAnnounceInfo() (announceIP string, announcePort int, err error) {
	if s.dockerURL == "" || s.containerName == "" {
		// Docker not configured, do nothing
		return s.defaultAnnounceIP, s.defaultAnnouncePort, nil
	}

	s.log.Info("Fetching announce info from docker")
	dockerClient, err := docker.NewClient(s.dockerURL)
	if err != nil {
		return "", 0, maskAny(err)
	}

	inspect := func() error {
		c, err := dockerClient.InspectContainer(s.containerName)
		if err != nil {
			return maskAny(err)
		}
		if c.NetworkSettings != nil {
			if binding, ok := c.NetworkSettings.Ports[redisPort]; ok {
				if len(binding) > 0 {
					announceIP = binding[0].HostIP
					if port, err := strconv.Atoi(binding[0].HostPort); err != nil {
						return maskAny(err)
					} else {
						announcePort = port
					}
					s.log.Infof("Found announce info: '%s:%d'", announceIP, announcePort)
					return nil
				}
			}
		}
		return maskAny(fmt.Errorf("Port binding not found for '%s'", redisPort))
	}

	err = retry.Do(inspect,
		retry.Timeout(time.Second*15),
		retry.MaxTries(3),
		retry.Sleep(time.Second*2))
	if err != nil {
		return "", 0, maskAny(err)
	}

	return announceIP, announcePort, nil
}
