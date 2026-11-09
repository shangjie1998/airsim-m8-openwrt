package audio

import "sync"

const (
	uplinkQueueFrames = 12 // 240 ms maximum queued speech.
	uplinkPrimeTicks  = 2  // 40 ms cushion before draining a new burst stream.
)

// iOS can deliver a 100 ms microphone callback as five 20 ms frames at once.
// Accept those bursts intact, then consume at the hardware's 20 ms cadence.
// A bounded queue drops the oldest frame only if sustained backlog exceeds it.
type uplinkQueue struct {
	mu      sync.Mutex
	frames  [uplinkQueueFrames][]byte
	head    int
	size    int
	prime   int
	started bool
}

func (q *uplinkQueue) push(frame []byte) (dropped bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.started {
		q.started = true
		q.prime = uplinkPrimeTicks
	}
	if q.size == len(q.frames) {
		q.frames[q.head] = nil
		q.head = (q.head + 1) % len(q.frames)
		q.size--
		dropped = true
	}
	q.frames[(q.head+q.size)%len(q.frames)] = frame
	q.size++
	return dropped
}

func (q *uplinkQueue) next() []byte {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.size == 0 {
		q.started = false
		q.prime = 0
		return nil
	}
	if q.prime > 0 {
		q.prime--
		return nil
	}
	frame := q.frames[q.head]
	q.frames[q.head] = nil
	q.head = (q.head + 1) % len(q.frames)
	q.size--
	return frame
}
