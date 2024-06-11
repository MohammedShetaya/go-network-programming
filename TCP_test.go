package go_network_programming

import (
	"io"
	"net"
	"syscall"
	"testing"
	"time"
)

func TestListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = listener.Close()
	}()
	t.Logf("listening on %s", listener.Addr().String())

	for {
		conn, err := listener.Accept()
		if err != nil {
			t.Fatal(err)
			return
		}

		go func(c net.Conn) {
			defer func() {
				_ = c.Close()
			}()
			t.Logf("Accepted connection from %s", c.RemoteAddr().String())
		}(conn)
	}
}

func TestDial(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:")
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Listening on %s", listener.Addr())
	done := make(chan struct{})

	go func() {
		// sent the signal to close the connection
		defer func() { done <- struct{}{} }()

		for {
			// accept the connection
			conn, err := listener.Accept()

			if err != nil {
				done <- struct{}{}
				return
			}

			t.Logf("Accepted a TCP connection from %s", conn.RemoteAddr())

			// handle received connections
			go func(c net.Conn) {
				// close the connection at the end
				defer func() {
					err := c.Close()
					if err != nil {
						// error closing the connection
						t.Error(err)
						return
					}
					// once one connection is handled, close
					done <- struct{}{}
				}()

				// read 1024 byte chunks until no more
				buff := make([]byte, 1024)

				for {
					n, err := c.Read(buff)
					if err != nil {
						if err != io.EOF {
							t.Error(err)
						}
						return
					}
					t.Logf("received: %q", buff[:n])
				}
			}(conn)
		}

	}()

	// initiate the connection with the listener
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	_, err = conn.Write([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	if err != nil {
		return
	}

	// Close the connection and wait for the first signal
	conn.Close()
	<-done

	// Close the listener and wait for the second signal
	listener.Close()
	<-done
}

func DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{
		Control: func(network, address string, c syscall.RawConn) error {
			return &net.DNSError{
				Err:         "connection timed out",
				Name:        address,
				Server:      "127.0.0.1",
				IsTimeout:   true,
				IsTemporary: true,
			}
		},
		Timeout: timeout,
	}
	return d.Dial(network, address)
}
func TestDialTimeout(t *testing.T) {
	_, err := DialTimeout("tcp", "10.0.0.1:http", 5*time.Second)

	t.Logf(err.Error())
	if err == nil {
		t.Fatal("No Timeout Occurred")
	}

	// type assertion that the received is network err
	nErr, ok := err.(net.Error)
	if !ok {
		t.Fatal(err)
	}

	if !nErr.Timeout() {
		t.Fatal("Not a Timeout Err")
	}
}
