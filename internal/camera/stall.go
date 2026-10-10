package camera

import (
	"sync"
	"time"

	"github.com/IDisposable/docker-wyze-bridge/internal/go2rtcmgr"
)

// StallTracker detects go2rtc streams that have RTSP/WebRTC consumers but
// are not delivering increasing media bytes (configured producers with no
// active session, or a connected producer whose byte counter is flat).
type StallTracker struct {
	mu sync.Mutex

	states map[string]*stallCamState

	StreakThreshold int
	WebRTCCooldown  time.Duration
	TUTKCooldown    time.Duration
	Now             func() time.Time
}

type stallCamState struct {
	lastBytes    int64
	streak       int
	preloadFails int
	lastRestart  time.Time
}

// NewStallTracker returns a tracker with production defaults.
func NewStallTracker() *StallTracker {
	return &StallTracker{
		states:          make(map[string]*stallCamState),
		StreakThreshold: 2,
		WebRTCCooldown:  3 * time.Minute,
		TUTKCooldown:    45 * time.Second,
		Now:             time.Now,
	}
}

// StallDecision describes whether HealthCheck should restart a stream.
type StallDecision struct {
	Restart   bool
	Consumers int
	Bytes     int64
	Streak    int
}

func (t *StallTracker) cooldown(protocol string) time.Duration {
	if protocol == "webrtc" {
		return t.WebRTCCooldown
	}
	return t.TUTKCooldown
}

func (t *StallTracker) stateFor(name string) *stallCamState {
	if st, ok := t.states[name]; ok {
		return st
	}
	st := &stallCamState{}
	t.states[name] = st
	return st
}

func streamHasConnectedProducer(info *go2rtcmgr.StreamInfo) bool {
	for _, p := range info.Producers {
		if p.Connected() {
			return true
		}
	}
	return false
}

// Evaluate applies one health-check tick for a streaming camera. Call only
// when the stream exists and len(Producers) > 0 — missing/empty streams keep
// the legacy HealthCheck path. preloaded is true when go2rtc preload is
// configured for this camera: a configured-but-unconnected producer must
// recover even with zero RTSP/WebRTC consumers (preload alone wants a dial).
func (t *StallTracker) Evaluate(name, protocol string, info *go2rtcmgr.StreamInfo, preloaded bool) StallDecision {
	t.mu.Lock()
	defer t.mu.Unlock()

	st := t.stateFor(name)
	consumers := len(info.Consumers)
	bytes := info.ReceivedBytes()
	connected := streamHasConnectedProducer(info)

	if consumers == 0 && !preloaded {
		st.streak = 0
		return StallDecision{}
	}

	var stalling bool
	switch {
	case consumers == 0 && preloaded:
		stalling = !connected
	default:
		stalling = !connected || bytes <= st.lastBytes
	}
	if stalling {
		st.streak++
	} else {
		st.streak = 0
		st.lastBytes = bytes
		return StallDecision{Consumers: consumers, Bytes: bytes}
	}

	threshold := t.StreakThreshold
	if threshold <= 0 {
		threshold = 2
	}
	dec := StallDecision{
		Consumers: consumers,
		Bytes:     bytes,
		Streak:    st.streak,
	}
	if st.streak < threshold {
		return dec
	}

	since := t.Now().Sub(st.lastRestart)
	if since < t.cooldown(protocol) {
		return dec
	}

	st.lastRestart = t.Now()
	st.streak = 0
	st.lastBytes = 0
	dec.Restart = true
	return dec
}

// Reset clears stall tracking for a camera (e.g. after manual stop).
func (t *StallTracker) Reset(name string) {
	t.mu.Lock()
	delete(t.states, name)
	t.mu.Unlock()
}

// PreloadFailureDecision is returned when AddPreload fails.
type PreloadFailureDecision struct {
	Restart  bool
	Failures int
}

// RecordPreloadSuccess clears consecutive preload failure count.
func (t *StallTracker) RecordPreloadSuccess(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stateFor(name).preloadFails = 0
}

// RecordPreloadFailure increments preload failures and may request a stream
// restart using the same cooldown as byte-stall restarts.
func (t *StallTracker) RecordPreloadFailure(name, protocol string) PreloadFailureDecision {
	t.mu.Lock()
	defer t.mu.Unlock()

	st := t.stateFor(name)
	st.preloadFails++
	dec := PreloadFailureDecision{Failures: st.preloadFails}
	if st.preloadFails < 2 {
		return dec
	}
	if t.Now().Sub(st.lastRestart) < t.cooldown(protocol) {
		return dec
	}
	st.lastRestart = t.Now()
	st.preloadFails = 0
	st.streak = 0
	st.lastBytes = 0
	dec.Restart = true
	return dec
}
