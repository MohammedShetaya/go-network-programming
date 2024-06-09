package go_network_programming

import (
	"net"
	"testing"
)

func TestLister(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = listener.Close()
	}()
	t.Logf("listening on %s", listener.Addr().String())
}
