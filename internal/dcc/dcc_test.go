package dcc

import (
	"net"
	"testing"
)

func TestIPToInt(t *testing.T) {
	n, err := IPToInt("2.31.76.192")
	if err != nil {
		t.Fatal(err)
	}
	if n != 35605696 {
		t.Fatalf("IPToInt = %d, want 35605696", n)
	}
}

func TestIPFromInt(t *testing.T) {
	if got := IPFromInt(35605696); got != "2.31.76.192" {
		t.Fatalf("IPFromInt = %q, want 2.31.76.192", got)
	}
}

func TestBuildDCCSend(t *testing.T) {
	got := BuildDCCSend("a.bin", 35605696, 30000, 28)
	want := "\x01DCC SEND a.bin 35605696 30000 28\x01"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildPassiveDCCSend(t *testing.T) {
	got := BuildPassiveDCCSend("a.bin", 35605696, 28, 7)
	want := "\x01DCC SEND a.bin 35605696 0 28 7\x01"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildDCCAccept(t *testing.T) {
	if got, want := BuildDCCAccept("a.bin", "30000", 128, ""), "\x01DCC ACCEPT a.bin 30000 128\x01"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := BuildDCCAccept("a.bin", "30000", 128, "42"), "\x01DCC ACCEPT a.bin 30000 128 42\x01"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestConfigureTCP(t *testing.T) {
	// Real TCP conn: sets nodelay + buffers without error.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		c, err := ln.Accept()
		if err == nil {
			ConfigureTCP(c, TCPConfig{NoDelay: true, BufferSize: 64 * 1024})
			c.Close()
		}
		close(done)
	}()

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	ConfigureTCP(c, TCPConfig{NoDelay: true, BufferSize: 64 * 1024})
	c.Close()
	<-done

	// Non-TCP conn (net.Pipe) is a safe no-op.
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	ConfigureTCP(a, TCPConfig{NoDelay: true, BufferSize: 1024})
}
