package proxy

type Proxy interface {
	// Update the proxy, using given master.
	Update(masterURL string) error
}
