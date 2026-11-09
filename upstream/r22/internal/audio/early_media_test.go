package audio

import (
	"context"
	"encoding/binary"
	"io"
	"sync"
	"testing"
	"time"
)

func TestEarlyCaptureOnlyStreamsSourceAndSharesDuplexReservation(t *testing.T) {
	b := &Bridge{}
	raw, writer := io.Pipe()
	cleaned := make(chan struct{})
	stream, err := b.openEarlyMedia(context.Background(), func(context.Context) (io.ReadCloser, func(), error) { return raw, func() { close(cleaned) }, nil })
	if err != nil {
		t.Fatal(err)
	}
	if !b.busy.Load() {
		t.Fatal("reservation missing")
	}
	if second, e := b.openEarlyMedia(context.Background(), nil); e == nil {
		second.Close()
		t.Fatal("second capture admitted")
	}
	got := make(chan []byte, 1)
	go func() { frame := make([]byte, 320); _, _ = io.ReadFull(stream, frame); got <- frame }()
	select {
	case <-got:
		t.Fatal("audio fabricated before source data")
	case <-time.After(30 * time.Millisecond):
	}
	pcm := make([]byte, 960*4)
	for i := 0; i < len(pcm); i += 2 {
		binary.LittleEndian.PutUint16(pcm[i:], 12000)
	}
	if _, err = writer.Write(pcm); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-got:
		nonzero := false
		for _, v := range frame {
			nonzero = nonzero || v != 0
		}
		if !nonzero {
			t.Fatal("source not delivered")
		}
	case <-time.After(time.Second):
		t.Fatal("downlink missing")
	}
	var group sync.WaitGroup
	for i := 0; i < 3; i++ {
		group.Add(1)
		go func() { defer group.Done(); stream.Close() }()
	}
	group.Wait()
	writer.Close()
	select {
	case <-cleaned:
	default:
		t.Fatal("returned before resource cleanup")
	}
	if b.busy.Load() || b.RX.Load() != 0 || b.Received.Load() != 0 || b.Silence.Load() != 0 {
		t.Fatal("uplink/silence or reservation leak")
	}
}
func TestEarlyCaptureCancellationUnblocksStalledCapture(t *testing.T) {
	b := &Bridge{}
	raw, writer := io.Pipe()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := b.openEarlyMedia(ctx, func(context.Context) (io.ReadCloser, func(), error) { return raw, func() {}, nil })
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	done := make(chan struct{})
	go func() { stream.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("capture cancellation hung")
	}
	if b.busy.Load() {
		t.Fatal("resource not released")
	}
}
