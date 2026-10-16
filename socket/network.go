package socket

import (
	"errors"
	"net"
)

// NetworkOptions configures the underlying TCP socket, including through TLS.
// Zero buffer sizes and nil NoDelay preserve the Go/OS defaults.
type NetworkOptions struct {
	ReadBuffer, WriteBuffer int
	NoDelay                 *bool
}

func (o NetworkOptions) normalized() (NetworkOptions, error) {
	if o.ReadBuffer < 0 || o.WriteBuffer < 0 {
		return o, errors.New("socket: negative network buffer")
	}
	if o.NoDelay != nil {
		value := *o.NoDelay
		o.NoDelay = &value
	}
	return o, nil
}
func ConfigureConn(conn net.Conn, options NetworkOptions) error {
	o, err := options.normalized()
	if err != nil {
		return err
	}
	if o.ReadBuffer == 0 && o.WriteBuffer == 0 && o.NoDelay == nil {
		return nil
	}
	if tlsConn, ok := conn.(interface{ NetConn() net.Conn }); ok {
		conn = tlsConn.NetConn()
	}
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return errors.New("socket: network options require TCP")
	}
	if o.ReadBuffer > 0 {
		if err := tcp.SetReadBuffer(o.ReadBuffer); err != nil {
			return err
		}
	}
	if o.WriteBuffer > 0 {
		if err := tcp.SetWriteBuffer(o.WriteBuffer); err != nil {
			return err
		}
	}
	if o.NoDelay != nil {
		return tcp.SetNoDelay(*o.NoDelay)
	}
	return nil
}
func (s *Session) LocalAddr() net.Addr {
	if conn, ok := s.transport.(interface{ LocalAddr() net.Addr }); ok {
		return conn.LocalAddr()
	}
	return nil
}
func (s *Session) RemoteAddr() net.Addr {
	if conn, ok := s.transport.(interface{ RemoteAddr() net.Addr }); ok {
		return conn.RemoteAddr()
	}
	return nil
}
