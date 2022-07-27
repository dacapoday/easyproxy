package socket

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v4"
)

var ErrServerClosed = errors.New("socket: Server closed")

type Context struct {
	net.Conn        // by conn
	context.Context // by Serve
}

type Handler func(*Context) //close socket when return

type Server struct {
	BaseContext  func(net.Listener) context.Context
	ErrorHandler func(*Context, ...interface{})
	m            sync.Mutex
	listeners    map[*net.Listener]struct{}
	open         int32
	closed       int32
}

func (server *Server) trackListener(listener *net.Listener, track bool) bool {
	server.m.Lock()
	defer server.m.Unlock()
	if track {
		if server.isClosed() {
			return false
		}
		if server.listeners == nil {
			server.listeners = make(map[*net.Listener]struct{})
		}
		server.listeners[listener] = struct{}{}
	} else {
		delete(server.listeners, listener)
		(*listener).Close()
	}
	return true
}

func (server *Server) Serve(listener net.Listener, handler Handler) (err error) {
	// listener = onceCloseListener{Listener: listener}
	if !server.trackListener(&listener, true) {
		return ErrServerClosed
	}
	defer server.trackListener(&listener, false)

	baseCtx := context.Background()
	if server.BaseContext != nil {
		baseCtx = server.BaseContext(listener)
		if baseCtx == nil {
			panic("ServeContext returned a nil context")
		}
	}

	atomic.AddInt32(&server.open, 1)
	defer atomic.AddInt32(&server.open, -1)

	var conn net.Conn
	for {
		conn, err = listener.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				time.Sleep(time.Second)
				continue
			}
			if server.isClosed() {
				err = ErrServerClosed
				return
			}
			return
		}
		go server.serve(baseCtx, conn, handler)
	}
}

func (server *Server) serve(ctx context.Context, conn net.Conn, handler Handler) {
	atomic.AddInt32(&server.open, 1)
	defer atomic.AddInt32(&server.open, -1)

	defer conn.Close()

	// var cancel context.CancelFunc
	// ctx, cancel = context.WithCancel(ctx)
	// defer cancel()

	socket := &Context{conn, ctx}
	defer func() {
		if server.ErrorHandler != nil {
			if err := recover(); err != nil {
				server.ErrorHandler(socket, err)
			}
		}
	}()

	if handler != nil {
		handler(socket)
	}
}

func (server *Server) isClosed() bool {
	return atomic.LoadInt32(&server.closed) != 0
}

func (server *Server) Close() (err error) {
	atomic.StoreInt32(&server.closed, 1)
	server.m.Lock()
	defer server.m.Unlock()

	for listener := range server.listeners {
		if cerr := (*listener).Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	server.listeners = nil

	return
}

func (server *Server) Shutdown(ctx context.Context) (err error) {
	if err = server.Close(); err != nil {
		return
	}

	ticker := backoff.NewTicker(NewNeverStopBackOff())
	for range ticker.C {
		if open := atomic.LoadInt32(&server.open); open == 0 {
			break
		}
		if err = ctx.Err(); err != nil {
			break
		}
	}
	ticker.Stop()
	return
}

func NewNeverStopBackOff() *backoff.ExponentialBackOff {
	b := &backoff.ExponentialBackOff{
		InitialInterval:     backoff.DefaultInitialInterval,
		RandomizationFactor: backoff.DefaultRandomizationFactor,
		Multiplier:          backoff.DefaultMultiplier,
		MaxInterval:         backoff.DefaultMaxInterval,
		MaxElapsedTime:      0,
		Stop:                backoff.Stop,
		Clock:               backoff.SystemClock,
	}
	b.Reset()
	return b
}

// type onceCloseListener struct {
// 	net.Listener
// 	once     sync.Once
// 	closeErr error
// }

// func (oc *onceCloseListener) Close() error {
// 	oc.once.Do(oc.close)
// 	return oc.closeErr
// }

// func (oc *onceCloseListener) close() { oc.closeErr = oc.Listener.Close() }
