package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/soheilhy/cmux"

	"github.com/dacapoday/easyproxy/socket"
	"github.com/dacapoday/httpproxy"
)

/* global object begin */
var logger zerolog.Logger

var build = "unknown"

func init() {
	fmt.Println("build:", build)

	logger = log.Logger
}

/* global object end */

func main() {
	allowdIPs := ""
	if allow_ip := os.Getenv("ALLOW_IP"); allow_ip != "" {
		allowdIPs = allow_ip
		logger.Printf("Allowed IP: %v\n", allowdIPs)
	}

	addr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}
	logger.Printf("Listening on %v\n", addr)

	l, err := net.Listen("tcp", addr)
	if err != nil {
		panic(err)
	}

	if allowdIPs != "" {
		l = socket.AllowedIPSubnet(allowdIPs)(l)
	}

	m := cmux.New(l)
	socksListener := m.Match(cmux.PrefixMatcher("\x05"))
	anyListener := m.Match(cmux.Any())

	httpProxy := &httpproxy.Proxy{}
	httpServer := Server{}
	go httpServer.Serve(anyListener, httpLog(&logger)(httpProxy))

	var dialer net.Dialer
	socksProxy := socket.Proxy{Dial: socksLog(&logger)(dialer.DialContext)}.Socks5Connect
	socketServer := socket.Server{}
	go socketServer.Serve(socksListener, socksProxy)

	m.Serve()
}

type dialer = func(ctx context.Context, network string, addr string) (net.Conn, error)

func socksLog(logger *zerolog.Logger) func(dialer) dialer {
	return func(dial dialer) dialer {
		return func(ctx context.Context, network, addr string) (net.Conn, error) {
			logger.Printf("Dialing %v %v\n", network, addr)
			return dial(ctx, network, addr)
		}
	}
}

func httpLog(logger *zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger.Printf("Request %v \n", r.URL.String())
			next.ServeHTTP(w, r)
		})
	}
}

type Server http.Server

func (server *Server) Serve(listener net.Listener, handler http.Handler) error {
	server.Handler = handler
	return (*http.Server)(server).Serve(listener)
}

func (server *Server) Close() error {
	return (*http.Server)(server).Close()
}
