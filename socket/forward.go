package socket

import (
	"io"
	"net"
)

func NewForwardHandler(open func(socket *Context) (net.Conn, error)) Handler {
	return Handler(func(source *Context) {
		target, err := open(source)
		if err != nil {
			return
		}
		defer target.Close()

		done := make(chan struct{})
		go copy(source, target, done)
		go copy(target, source, done)

		c := 2
		for {
			select {
			case <-source.Done():
				close(done)
				return
			case done <- struct{}{}:
				c--
				if c == 0 {
					return
				}
			}
		}
	})
}

func copy(to io.Writer, from io.Reader, done <-chan struct{}) {
	io.Copy(to, from)
	<-done
}
