package go_network_programming

import (
	"context"
	_ "context"
	"io"
	"net"
	"sync"
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

func TestTimeoutContext(t *testing.T) {
	dl := time.Now().Add(5 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), dl)

	defer cancel()

	var dialer net.Dialer

	dialer.Control = func(network, address string, c syscall.RawConn) error {
		// the total time is the time to connect to host and the sleep time below.
		time.Sleep(5*time.Second + time.Millisecond)
		//t.Logf("shouldn't reach this point")
		return nil
	}

	conn, err := dialer.DialContext(ctx, "tcp", "10.0.0.1:80")

	if err == nil {
		conn.Close()
		t.Fatal("No Timeout Occurred")
	}

	nErr, ok := err.(net.Error)

	if !ok {
		t.Error(err)
	} else {
		if !nErr.Timeout() {
			t.Fatal("Error Not a Timeout")
		}
	}

	if ctx.Err() != context.DeadlineExceeded {
		t.Errorf("expected deadline exceeded; actual: %v", ctx.Err())
	}
}

func TestDialContextCancelFanOut(t *testing.T) {
	ctx, cancel := context.WithDeadline(
		context.Background(),
		time.Now().Add(10*time.Second),
	)

	listener, err := net.Listen("tcp", "127.0.0.1:")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() {
		// Only accepting a single connection.
		conn, err := listener.Accept()
		if err == nil {
			conn.Close()
		}
	}()

	dial := func(ctx context.Context, address string, response chan int,
		id int, wg *sync.WaitGroup) {
		defer wg.Done()

		var d net.Dialer
		c, err := d.DialContext(ctx, "tcp", address)
		if err != nil {
			return
		}
		c.Close()

		// send the id over the response chan if context was not cancelled
		select {
		// if the context is cancelled
		case <-ctx.Done():
		// only one go routine will get to exec this line.
		case response <- id:
		}
	}

	res := make(chan int)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go dial(ctx, listener.Addr().String(), res, i+1, &wg)
	}

	// capture only one of the responses then cancel.
	response := <-res
	t.Logf("Response is: %d", response)
	cancel()
	// wait for all for go routines to terminate after closing the context.
	wg.Wait()
	close(res)

	if ctx.Err() != context.Canceled {
		t.Errorf("expected canceled context; actual: %s",
			ctx.Err(),
		)
	}

	t.Logf("dialer %d retrieved the resource", response)
}

func TestHeartBeatWithDeadline(t *testing.T) {
	/*
		To have a heart beat mechanism that is sent only if the timer is expired.
		if data is sent from any of the two ends the timer should be reset to avoid unnecessary heart beats to be sent.
	*/

	ctx, cancel := context.WithCancel(context.Background())
	// cancel the whole context
	defer cancel()

	var listenerReset = make(chan time.Duration, 1)
	var clientReset = make(chan time.Duration, 1)
	const defaultTimeout = 10 * time.Second

	// create a listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")

	if err != nil {
		t.Fatal(err)
	}

	// handles automatic timeouts and pings
	ping := func(ctx context.Context, reset chan time.Duration, conn net.Conn) {

		// default timeout
		var timeout = defaultTimeout

		timer := time.NewTimer(timeout)

		defer func() {
			if !timer.Stop() {
				<-timer.C
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case newTimeout := <-reset:
				// if the timer expired drain the timer channel
				if !timer.Stop() {
					<-timer.C
				}
				// if the new timeout is valid make it the new timerout starting from now.
				if newTimeout > 0 {
					timeout = newTimeout
				}
			// if the timer expired send a ping message
			case <-timer.C:
				t.Logf("Heartbeat timedout. Pinging %s", conn.RemoteAddr())
				_, err := conn.Write([]byte{1})
				if err != nil {
					t.Logf("Couldn't ping remote: %s", conn.RemoteAddr())
				}
			}
			timer.Reset(timeout)
		}
	}
	// handles the r/w operations of the connection
	handleConnection := func(ctx context.Context, reset chan time.Duration, conn net.Conn) {
		defer func() {
			conn.Close()
		}()

		buff := make([]byte, 1024)

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			r, err := conn.Read(buff)
			if err != nil {
				if err != io.EOF {
					t.Fatal(err)
				}
			}
			t.Logf("Received from %s:\n%q \n", conn.RemoteAddr(), buff[:r])
			// if we received a message then remote client is alive. Reset timeout.
			reset <- defaultTimeout
		}
	}
	// handle incoming connections
	go func() {
		defer listener.Close()

		for {

			select {
			case <-ctx.Done():
				return
			default:
			}

			conn, err := listener.Accept()

			if err != nil {
				t.Fatal(err)
			}

			go ping(ctx, listenerReset, conn)
			go handleConnection(ctx, listenerReset, conn)
		}
	}()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	go ping(ctx, clientReset, conn)
	// handle messages sent from listener to client
	go handleConnection(ctx, clientReset, conn)

	_, err = conn.Write([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	if err != nil {
		return
	}

	// wait long enough to observe the communication
	time.Sleep(40 * time.Second)

}
