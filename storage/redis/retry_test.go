package redis

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Simulate a committed INCR whose reply is lost. The caller cannot know whether
// it committed; retrying changes state twice even though Call returns success.
func TestAmbiguousWriteReplayRequiresOptIn(t *testing.T) {
	for _, retries := range []int{0, 1} {
		t.Run(fmt.Sprint(retries), func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			var writes atomic.Int32
			var wg sync.WaitGroup
			wg.Go(func() {
				for {
					conn, err := ln.Accept()
					if err != nil {
						return
					}
					wg.Go(func() {
						defer conn.Close()
						r := bufio.NewReader(conn)
						for {
							args, err := readCommand(r)
							if err != nil {
								return
							}
							switch strings.ToUpper(args[0]) {
							case "HELLO":
								fmt.Fprint(conn, "-ERR unknown command 'HELLO'\r\n")
							case "PING":
								fmt.Fprint(conn, "+PONG\r\n")
							case "INCR":
								n := writes.Add(1)
								if n == 1 {
									return
								}
								fmt.Fprintf(conn, ":%d\r\n", n)
							default:
								fmt.Fprint(conn, "+OK\r\n")
							}
						}
					})
				}
			})
			defer func() { ln.Close(); wg.Wait() }()
			c, err := Open(t.Context(), Options{Addresses: []string{ln.Addr().String()}, Retries: retries})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			value, err := c.Incr(t.Context(), "counter").Result()
			if retries == 0 {
				if err == nil || writes.Load() != 1 {
					t.Fatalf("ambiguous write replayed: value=%d writes=%d err=%v", value, writes.Load(), err)
				}
			} else if err != nil || value != 2 || writes.Load() != 2 {
				t.Fatalf("explicit retry failed: value=%d writes=%d err=%v", value, writes.Load(), err)
			}
		})
	}
}

func readCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(line, "*") {
		return nil, fmt.Errorf("expected array")
	}
	n, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil || n < 1 || n > 16 {
		return nil, fmt.Errorf("bad command")
	}
	args := make([]string, n)
	for i := range n {
		line, err = r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(line, "$") {
			return nil, fmt.Errorf("expected bulk")
		}
		length, err := strconv.Atoi(strings.TrimSpace(line[1:]))
		if err != nil || length < 0 || length > 1024 {
			return nil, fmt.Errorf("bad bulk")
		}
		data := make([]byte, length+2)
		if _, err = io.ReadFull(r, data); err != nil {
			return nil, err
		}
		args[i] = string(data[:length])
	}
	return args, nil
}
