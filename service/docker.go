package service

import (
	"fmt"
	"strconv"
	"time"

	"github.com/fsouza/go-dockerclient"
	"github.com/giantswarm/retry-go"
)

const (
	redisPort = "6379/tcp"
)

// fetchAnnounceInfoFromContainer connects to docker to ask for the announce IP
// and port for the configured container name.
func (s *Service) fetchAnnounceInfoFromContainer() error {
	if s.DockerURL == "" || s.ContainerName == "" {
		// Docker not configured, do nothing
		return nil
	}

	s.Logger.Info("Fetching announce info from docker")
	dockerClient, err := docker.NewClient(s.DockerURL)
	if err != nil {
		return maskAny(err)
	}

	inspect := func() error {
		c, err := dockerClient.InspectContainer(s.ContainerName)
		if err != nil {
			return maskAny(err)
		}
		if c.NetworkSettings != nil {
			if binding, ok := c.NetworkSettings.Ports[redisPort]; ok {
				if len(binding) > 0 {
					s.AnnounceIP = binding[0].HostIP
					if port, err := strconv.Atoi(binding[0].HostPort); err != nil {
						return maskAny(err)
					} else {
						s.AnnouncePort = port
					}
					s.Logger.Infof("Found announce info: '%s:%d'", s.AnnounceIP, s.AnnouncePort)
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
		return maskAny(err)
	}

	return nil
}
