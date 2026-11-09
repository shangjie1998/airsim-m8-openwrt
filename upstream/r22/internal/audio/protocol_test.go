package audio

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestInvalidHandshakeDoesNotAcquireAudio(t *testing.T) {
	b := &Bridge{}
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() { b.Handle(context.Background(), server); close(done) }()
	client.Write([]byte("NOTAIRSIM1\n"))
	client.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handshake leaked")
	}
	if b.busy.Load() {
		t.Fatal("invalid handshake acquired hardware")
	}
}
func TestBusyBridgeRejectsSecondSession(t *testing.T) {
	b := &Bridge{}
	b.busy.Store(true)
	server, client := net.Pipe()
	go b.Handle(context.Background(), server)
	client.SetDeadline(time.Now().Add(time.Second))
	client.Write([]byte("AIRSIMPCM1\n"))
	var ack [10]byte
	if _, e := io.ReadFull(client, ack[:]); e == nil {
		t.Fatal("concurrent media accepted")
	}
	client.Close()
}
