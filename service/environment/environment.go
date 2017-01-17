package environment

type Environment interface {
	// FetchAnnounceInfo fetches the IP address of the current process
	// and the port number that will be announced to other instances.
	FetchAnnounceInfo() (announceIP string, announcePort int, err error)
}
