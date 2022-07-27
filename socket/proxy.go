package socket

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"strconv"
	"time"
)

type Proxy struct {
	Dial    func(ctx context.Context, network, addr string) (net.Conn, error)
	Timeout time.Duration
}

// SocksConnect support socks5, socks4 and socks4a, but NoAuth and only CONNECT command
// func (proxy Proxy) SocksConnect(socket *Context) {}

// Socks4Connect support socks4 and socks4a, but NoAuth and only CONNECT command
// func (proxy Proxy) Socks4Connect(socket *Context) {}

// Socks5Connect only support socks5 NoAuth and only CONNECT command
func (proxy Proxy) Socks5Connect(socket *Context) {
	var err error
	buf := [3]byte{}
	if _, err = io.ReadFull(socket, buf[:2]); err != nil {
		err = ERR_READ_FAILED
		return
	}

	if buf[0] != Socks5Version {
		err = ERR_VERSION
		return
	}

	if _, err = io.CopyN(ioutil.Discard, socket, int64(buf[1])); err != nil {
		err = ERR_METHOD
		return
	}

	buf[1] = MethodNoAuth
	if _, err = socket.Write(buf[:2]); err != nil {
		err = ERR_AUTH_FAILED
		return
	}

	if _, err = io.ReadFull(socket, buf[:]); err != nil {
		err = ERR_READ_FAILED
		return
	}

	if buf[0] != Socks5Version {
		err = ERR_VERSION
		return
	}
	rep := buf[1]

	addr := Address{}
	if err = addr.From(socket); err != nil {
		return
	}

	dial := proxy.Dial
	if dial == nil {
		var dialer net.Dialer
		dial = dialer.DialContext
	}

	var conn net.Conn
	if rep != CmdConnect {
		err = ERR_CMD
		buf[1] = 7 // Command not supported
	} else if conn, err = dial(socket, "tcp", addr.String()); err != nil {
		// todo: handle more err
		buf[1] = 3 // Connection unreachable
	} else {
		buf[1] = 0 // succeeded
	}

	// todo: convert domain to ip when relay

	if _, socket_err := socket.Write(buf[:]); socket_err != nil {
		return
	}
	if socket_err := addr.To(socket); socket_err != nil {
		return
	}
	if buf[1] != 0 {
		return
	}

	NewForwardHandler(func(*Context) (net.Conn, error) { return conn, nil })(socket)
}

const (
	Socks4Version = 4
	Socks5Version = 5
	MethodNoAuth  = 0
	CmdConnect    = 1
)

var (
	ERR_VERSION      = errors.New("unknown socks version")
	ERR_METHOD       = errors.New("unsupported method")
	ERR_CMD          = errors.New("unsupported command")
	ERR_READ_FAILED  = errors.New("read failed")
	ERR_ADDRESS_TYPE = errors.New("unsupported address type")
	ERR_AUTH_FAILED  = errors.New("auth failed")
)

type AddressType byte

const (
	IPv4       AddressType = 1
	DomainName AddressType = 3
	IPv6       AddressType = 4
)

type Address struct {
	NetworkType string
	DomainName  string
	net.IP
	Port int
	AddressType
}

func (a *Address) String() string {
	switch a.AddressType {
	case IPv4:
		return fmt.Sprintf("%s:%d", a.IP.String(), a.Port)
	case IPv6:
		return fmt.Sprintf("[%s]:%d", a.IP.String(), a.Port)
	case DomainName:
		return fmt.Sprintf("%s:%d", a.DomainName, a.Port)
	}
	return ""
}

func (a *Address) Network() string {
	return a.NetworkType
}

func (a *Address) ResolveIP() (net.IP, error) {
	if a.AddressType == IPv4 || a.AddressType == IPv6 {
		return a.IP, nil
	}
	if a.IP != nil {
		return a.IP, nil
	}
	addr, err := net.ResolveIPAddr("ip", a.DomainName)
	if err != nil {
		return nil, err
	}
	a.IP = addr.IP
	return addr.IP, nil
}

func NewAddressFromAddr(network string, addr string) (*Address, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.ParseInt(portStr, 10, 16)
	if err != nil {
		return nil, err
	}
	return NewAddressFromHostPort(network, host, int(port)), nil
}

func NewAddressFromHostPort(network string, host string, port int) *Address {
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			return &Address{
				IP:          ip,
				Port:        port,
				AddressType: IPv4,
				NetworkType: network,
			}
		}
		return &Address{
			IP:          ip,
			Port:        port,
			AddressType: IPv6,
			NetworkType: network,
		}
	}
	return &Address{
		DomainName:  host,
		Port:        port,
		AddressType: DomainName,
		NetworkType: network,
	}
}

func (a *Address) From(r io.Reader) (err error) {
	buf := [1]byte{}
	_, err = io.ReadFull(r, buf[:])
	if err != nil {
		return
	}
	a.AddressType = AddressType(buf[0])
	switch a.AddressType {
	case IPv4:
		var buf [6]byte
		_, err = io.ReadFull(r, buf[:])
		if err != nil {
			err = errors.New("failed to read IPv4")
			return
		}
		a.IP = buf[0:4]
		a.Port = int(binary.BigEndian.Uint16(buf[4:6]))
	case IPv6:
		var buf [18]byte
		_, err = io.ReadFull(r, buf[:])
		if err != nil {
			err = errors.New("failed to read IPv6")
			return
		}
		a.IP = buf[0:16]
		a.Port = int(binary.BigEndian.Uint16(buf[16:18]))
	case DomainName:
		_, err = io.ReadFull(r, buf[:])
		length := buf[0]
		if err != nil {
			err = errors.New("failed to read domain name length")
			return
		}
		buf := make([]byte, length+2)
		_, err = io.ReadFull(r, buf)
		if err != nil {
			err = errors.New("failed to read domain name")
			return
		}
		host := buf[0:length]
		if ip := net.ParseIP(string(host)); ip != nil {
			a.IP = ip
			if ip.To4() != nil {
				a.AddressType = IPv4
			} else {
				a.AddressType = IPv6
			}
		} else {
			a.DomainName = string(host)
		}
		a.Port = int(binary.BigEndian.Uint16(buf[length : length+2]))
	default:
		err = ERR_ADDRESS_TYPE
	}
	return
}

func (a *Address) To(w io.Writer) (err error) {
	_, err = w.Write([]byte{byte(a.AddressType)})
	if err != nil {
		return
	}
	switch a.AddressType {
	case DomainName:
		w.Write([]byte{byte(len(a.DomainName))})
		_, err = w.Write([]byte(a.DomainName))
	case IPv4:
		_, err = w.Write(a.IP.To4())
	case IPv6:
		_, err = w.Write(a.IP.To16())
	default:
		err = ERR_ADDRESS_TYPE
	}
	if err != nil {
		return
	}
	port := [2]byte{}
	binary.BigEndian.PutUint16(port[:], uint16(a.Port))
	_, err = w.Write(port[:])
	return
}
