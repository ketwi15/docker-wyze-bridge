package camera

import (
	"testing"
	"time"

	"github.com/IDisposable/docker-wyze-bridge/internal/go2rtcmgr"
)

func connectedTUTKStream(bytes int64, consumers int) *go2rtcmgr.StreamInfo {
	info := &go2rtcmgr.StreamInfo{
		Producers: []go2rtcmgr.ProducerInfo{{
			ID:         42,
			RemoteAddr: "192.168.4.40:48511",
			Receivers:  []go2rtcmgr.MediaCounter{{Bytes: bytes, Packets: 10}},
		}},
	}
	for i := 0; i < consumers; i++ {
		info.Consumers = append(info.Consumers, go2rtcmgr.ConsumerInfo{ID: 36 + i})
	}
	return info
}

func unconnectedConfiguredStream(consumers int) *go2rtcmgr.StreamInfo {
	info := &go2rtcmgr.StreamInfo{
		Producers: []go2rtcmgr.ProducerInfo{{URL: "wyze://192.168.4.40"}},
	}
	for i := 0; i < consumers; i++ {
		info.Consumers = append(info.Consumers, go2rtcmgr.ConsumerInfo{ID: 1 + i})
	}
	return info
}

func TestStallTracker_IdleNoConsumersNeverRestarts(t *testing.T) {
	tr := NewStallTracker()
	tr.StreakThreshold = 2
	tr.TUTKCooldown = time.Second

	s := connectedTUTKStream(1000, 0)
	if d := tr.Evaluate("cam", "tutk", s); d.Restart {
		t.Fatal("idle stream should not restart")
	}
	s.Producers[0].Receivers[0].Bytes = 1000
	if d := tr.Evaluate("cam", "tutk", s); d.Restart {
		t.Fatal("flat bytes without consumers should not restart")
	}
}

func TestStallTracker_IncreasingBytesNeverRestarts(t *testing.T) {
	tr := NewStallTracker()
	tr.StreakThreshold = 2
	tr.TUTKCooldown = time.Second

	s := connectedTUTKStream(100, 1)
	for i := 0; i < 5; i++ {
		s.Producers[0].Receivers[0].Bytes += 500
		if d := tr.Evaluate("cam", "tutk", s); d.Restart {
			t.Fatalf("tick %d: increasing bytes should not restart", i)
		}
	}
}

func TestStallTracker_FlatBytesRestartsAfterTwoTicks(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tr := NewStallTracker()
	tr.StreakThreshold = 2
	tr.TUTKCooldown = 45 * time.Second
	tr.Now = func() time.Time { return now }

	s := connectedTUTKStream(5000, 1)
	if d := tr.Evaluate("cam", "tutk", s); d.Restart || d.Streak != 0 {
		t.Fatalf("first tick with baseline bytes: %+v", d)
	}

	if d := tr.Evaluate("cam", "tutk", s); d.Restart || d.Streak != 1 {
		t.Fatalf("second tick flat: %+v", d)
	}
	if d := tr.Evaluate("cam", "tutk", s); !d.Restart {
		t.Fatalf("third tick should restart: %+v", d)
	}
}

func TestStallTracker_CooldownTUTK(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tr := NewStallTracker()
	tr.StreakThreshold = 2
	tr.TUTKCooldown = 45 * time.Second
	tr.Now = func() time.Time { return now }

	s := connectedTUTKStream(100, 1)
	tr.Evaluate("cam", "tutk", s)
	tr.Evaluate("cam", "tutk", s)
	if d := tr.Evaluate("cam", "tutk", s); !d.Restart {
		t.Fatal("expected first restart")
	}

	tr.Evaluate("cam", "tutk", s)
	tr.Evaluate("cam", "tutk", s)
	if d := tr.Evaluate("cam", "tutk", s); d.Restart {
		t.Fatal("restart within tutk cooldown should be suppressed")
	}

	now = now.Add(46 * time.Second)
	if d := tr.Evaluate("cam", "tutk", s); !d.Restart {
		t.Fatal("expected restart after tutk cooldown")
	}
}

func TestStallTracker_CooldownWebRTC(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tr := NewStallTracker()
	tr.StreakThreshold = 2
	tr.WebRTCCooldown = 3 * time.Minute
	tr.Now = func() time.Time { return now }

	s := &go2rtcmgr.StreamInfo{
		Producers: []go2rtcmgr.ProducerInfo{{
			URL:       "wss://wyze-mars-webcsrv.wyzecam.com",
			Receivers: []go2rtcmgr.MediaCounter{{Bytes: 100}},
		}},
		Consumers: []go2rtcmgr.ConsumerInfo{{ID: 1}},
	}
	tr.Evaluate("og", "webrtc", s)
	tr.Evaluate("og", "webrtc", s)
	if d := tr.Evaluate("og", "webrtc", s); !d.Restart {
		t.Fatal("expected first webrtc restart")
	}

	tr.Evaluate("og", "webrtc", s)
	tr.Evaluate("og", "webrtc", s)
	if d := tr.Evaluate("og", "webrtc", s); d.Restart {
		t.Fatal("restart within webrtc cooldown should be suppressed")
	}

	now = now.Add(3*time.Minute + time.Second)
	if d := tr.Evaluate("og", "webrtc", s); !d.Restart {
		t.Fatal("expected restart after webrtc cooldown")
	}
}

func TestStallTracker_PreloadFailureCooldown(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tr := NewStallTracker()
	tr.TUTKCooldown = 45 * time.Second
	tr.Now = func() time.Time { return now }

	if d := tr.RecordPreloadFailure("cam", "tutk"); d.Restart || d.Failures != 1 {
		t.Fatalf("first failure: %+v", d)
	}
	if d := tr.RecordPreloadFailure("cam", "tutk"); !d.Restart || d.Failures != 2 {
		t.Fatalf("second failure should restart: %+v", d)
	}

	tr.RecordPreloadFailure("cam", "tutk")
	if d := tr.RecordPreloadFailure("cam", "tutk"); d.Restart {
		t.Fatal("cooldown should block second restart")
	}

	now = now.Add(46 * time.Second)
	if d := tr.RecordPreloadFailure("cam", "tutk"); !d.Restart {
		t.Fatal("expected restart after cooldown")
	}
}

func TestStallTracker_PreloadSuccessResets(t *testing.T) {
	tr := NewStallTracker()
	tr.RecordPreloadFailure("cam", "tutk")
	tr.RecordPreloadSuccess("cam")
	if d := tr.RecordPreloadFailure("cam", "tutk"); d.Failures != 1 || d.Restart {
		t.Fatalf("success should reset counter: %+v", d)
	}
}

func TestStallTracker_UnconnectedProducerWithConsumerStalls(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tr := NewStallTracker()
	tr.StreakThreshold = 2
	tr.TUTKCooldown = time.Second
	tr.Now = func() time.Time { return now }

	s := unconnectedConfiguredStream(1)
	tr.Evaluate("cam", "tutk", s)
	if d := tr.Evaluate("cam", "tutk", s); !d.Restart {
		t.Fatalf("unconnected producer with consumer should restart: %+v", d)
	}
}
