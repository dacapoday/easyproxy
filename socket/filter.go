package socket

import (
	"net"
	"net/netip"
)

type Filter struct {
	Listener net.Listener
	Filter   func(net.Conn) bool
}

func (i *Filter) Accept() (conn net.Conn, err error) {
	for {
		conn, err = i.Listener.Accept()
		if err != nil {
			return
		}
		if conn == nil {
			return
		}
		if i.Filter(conn) {
			return
		}
		conn.Close()
	}
}

func (i *Filter) Close() error {
	return i.Listener.Close()
}

func (i *Filter) Addr() net.Addr {
	return i.Listener.Addr()
}

func AllowedIPSubnet(subnet string) func(net.Listener) *Filter {
	prefix := netip.MustParsePrefix(subnet)
	filter := func(conn net.Conn) bool {
		addrport, err := netip.ParseAddrPort(conn.RemoteAddr().String())
		if err != nil {
			return false
		}
		return prefix.Contains(addrport.Addr())
	}
	return func(ln net.Listener) *Filter {
		return &Filter{
			Listener: ln,
			Filter:   filter,
		}
	}
}
