package middleware

import (
	"net"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
)

func RunServer(host, port string) {
	http.Handle("/metrics", prometheus.Handler())

	addr := net.JoinHostPort(host, port)
	go http.ListenAndServe(addr, nil)
}
