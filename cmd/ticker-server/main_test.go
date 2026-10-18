package main

import "testing"

func TestOnlyLoopbackAddressesAreServed(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:50051", "[::1]:0", "localhost:9000"} {
		if err := loopback(ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"0.0.0.0:50051", ":50051", "10.0.0.5:1", "example.com:443", "no-port"} {
		if err := loopback(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}
