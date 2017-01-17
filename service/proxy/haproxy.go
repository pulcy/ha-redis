package proxy

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	logging "github.com/op/go-logging"
)

// NewHAProxy creates a new haproxy proxy implementation
func NewHAProxy(log *logging.Logger, port int, haProxyPath, haProxyConfPath string) (Proxy, error) {
	return &haProxy{
		log:             log,
		port:            port,
		haproxyPath:     haProxyPath,
		haproxyConfPath: haProxyConfPath,
	}, nil
}

type haProxy struct {
	log             *logging.Logger
	port            int
	haproxyConfPath string
	haproxyPath     string
	lastPid         int
	lastState       string
}

const (
	configPrefix = `defaults REDIS
	mode tcp
	timeout connect  4s
	timeout server  30s
	timeout client  30s
 
frontend ft_redis
	bind 0.0.0.0:%d name redis
	default_backend bk_redis
 
backend bk_redis
`
	serverTemplate = `	server %s %s`
)

// Update the proxy, using given master
func (p *haProxy) Update(masterURL string) error {
	// Sort and check if anything has changed
	state := masterURL
	if state == p.lastState {
		// Nothing has changed
		return nil
	}

	// Create config
	err := p.createConfigFile(masterURL)
	if err != nil {
		return maskAny(err)
	}

	// Restart HAProxy
	if err := p.restartHaproxy(); err != nil {
		return maskAny(err)
	}

	// Save state
	p.lastState = state

	return nil
}

// createConfigFile creates a temporary HAProxy config file and returns it path.
func (p *haProxy) createConfigFile(masterURL string) error {
	conf := fmt.Sprintf(configPrefix, p.port)
	for i, b := range []string{masterURL} {
		serverName := fmt.Sprintf("R%d", i)
		conf = conf + fmt.Sprintf(serverTemplate, serverName, b) + "\n"
	}
	os.MkdirAll(filepath.Dir(p.haproxyConfPath), 0755)
	if err := ioutil.WriteFile(p.haproxyConfPath, []byte(conf), 0644); err != nil {
		return maskAny(err)
	}
	return nil
}

// restartHaproxy restarts haproxy, killing previous instances
func (p *haProxy) restartHaproxy() error {
	args := []string{
		"-f",
		p.haproxyConfPath,
	}
	lastPid := p.lastPid
	if p.lastPid > 0 {
		args = append(args, "-sf", strconv.Itoa(p.lastPid))
	}

	p.log.Debugf("Starting haproxy with %#v", args)
	cmd := exec.Command(p.haproxyPath, args...)
	configureRestartHaproxyCmd(cmd)
	cmd.Stdin = bytes.NewReader([]byte{})
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		p.log.Errorf("Failed to start haproxy: %#v", err)
		return maskAny(err)
	}

	pid := -1
	proc := cmd.Process
	if proc != nil {
		pid = proc.Pid
	}
	p.lastPid = pid
	p.log.Debugf("haxproxy pid %d started", pid)

	go func() {
		// Wait for haproxy to terminate so we avoid defunct processes
		if err := cmd.Wait(); err != nil {
			p.log.Errorf("haproxy pid %d wait returned an error: %#v", pid, err)
		} else {
			p.log.Debugf("haproxy pid %d terminated", pid)
		}
	}()

	if lastPid != 0 {
		// Make sure the old haproxy terminates
		go func() {
			time.Sleep(time.Second * 10)
			p, _ := os.FindProcess(lastPid)
			if p != nil {
				p.Kill()
			}
		}()
	}

	return nil
}
