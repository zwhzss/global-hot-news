package router

import (
	"net"
	"strings"
	"testing"
)

func TestSetupLocalAPILoopsBackOnly(t *testing.T) {
	_, srv := SetupLocalAPI(0, "token")

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		t.Fatalf("listen on %q: %v", srv.Addr, err)
	}
	defer ln.Close()

	if addr := ln.Addr().String(); !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("local API bound to %s, want loopback only", addr)
	}
}
