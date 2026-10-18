package socket_test

import (
	"context"
	"github.com/NumberMan1/orbit/rpc"
	"github.com/NumberMan1/orbit/socket"
	"github.com/NumberMan1/orbit/socket/tcp"
	"net"
	"testing"
	"testing/synctest"
)

func TestCloseWaitsForAcceptedAsyncCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := net.Pipe()
		defer b.Close()
		started, canceled, release, closed := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		s, _ := socket.NewSession(t.Context(), tcp.Transport{Conn: a}, socket.Options{OnClose: func(context.Context, *socket.Session, error) { close(closed) }})
		handler, _ := socket.Async(1, func(ctx context.Context, _ *socket.Session, _ rpc.Frame) error {
			close(started)
			<-ctx.Done()
			close(canceled)
			<-release
			return nil
		})
		go s.Run(handler)
		if err := (tcp.Transport{Conn: b}).WriteFrame(rpc.Frame{ID: 1}, rpc.MaxPayload); err != nil {
			t.Fatal(err)
		}
		<-started
		s.Close()
		<-canceled
		synctest.Wait()
		select {
		case <-closed:
			t.Error("OnClose raced accepted callback cleanup")
		default:
		}
		select {
		case <-s.Done():
			t.Error("Run returned before callback cleanup")
		default:
		}
		close(release)
		<-s.Done()
		<-closed
	})
}
