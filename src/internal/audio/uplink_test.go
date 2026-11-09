package audio

import (
	"encoding/binary"
	"sync"
	"testing"
)

func numberedFrame(n int) []byte {
	b := make([]byte, FrameBytes)
	binary.LittleEndian.PutUint32(b, uint32(n))
	return b
}

func TestUplinkPreserves100msMicrophoneBursts(t *testing.T) {
	var q uplinkQueue
	var got []int
	silence, dropped := 0, 0
	// 20 seconds of audio delivered as five-frame batches every 100 ms,
	// including one 20 ms late batch followed by an on-time batch.
	for tick := 0; tick < 1000+uplinkPrimeTicks; tick++ {
		if tick < 1000 && tick%5 == 0 && tick != 500 || tick == 501 {
			batch := tick / 5
			for n := batch * 5; n < batch*5+5; n++ {
				if q.push(numberedFrame(n)) {
					dropped++
				}
			}
		}
		if frame := q.next(); frame != nil {
			got = append(got, int(binary.LittleEndian.Uint32(frame)))
		} else {
			silence++
		}
	}
	if dropped != 0 || silence != uplinkPrimeTicks || len(got) != 1000 {
		t.Fatalf("dropped=%d silence=%d played=%d", dropped, silence, len(got))
	}
	for i, n := range got {
		if n != i {
			t.Fatalf("speech discontinuity at %d: got frame %d", i, n)
		}
	}
}

func TestUplinkBacklogIsBoundedAndKeepsRecentSpeech(t *testing.T) {
	var q uplinkQueue
	dropped := 0
	for i := 0; i < 100; i++ {
		if q.push(numberedFrame(i)) {
			dropped++
		}
	}
	if dropped != 100-uplinkQueueFrames || q.size != uplinkQueueFrames {
		t.Fatalf("backlog is not bounded: dropped=%d size=%d", dropped, q.size)
	}
	for i := 0; i < uplinkPrimeTicks; i++ {
		if q.next() != nil {
			t.Fatal("missing initial cushion")
		}
	}
	for i := 100 - uplinkQueueFrames; i < 100; i++ {
		if got := int(binary.LittleEndian.Uint32(q.next())); got != i {
			t.Fatalf("got frame %d, want %d", got, i)
		}
	}
	if q.next() != nil {
		t.Fatal("empty queue returned speech")
	}
}

func TestUplinkReprimesAfterInterruption(t *testing.T) {
	var q uplinkQueue
	for episode := 0; episode < 2; episode++ {
		q.push(numberedFrame(episode))
		for i := 0; i < uplinkPrimeTicks; i++ {
			if q.next() != nil {
				t.Fatal("missing cushion after interruption")
			}
		}
		if q.next() == nil || q.next() != nil {
			t.Fatal("incorrect empty-queue transition")
		}
	}
}

func TestUplinkConcurrentProducerAndConsumer(t *testing.T) {
	var q uplinkQueue
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			q.push(numberedFrame(i))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			q.next()
		}
	}()
	wg.Wait()
	if q.size > uplinkQueueFrames {
		t.Fatal("queue overflow")
	}
}
