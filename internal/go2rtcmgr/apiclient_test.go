package go2rtcmgr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

func TestAPIClient_ListStreams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/streams" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Method != "GET" {
			t.Errorf("method = %q", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]*StreamInfo{
			"front_door": {
				Producers: []ProducerInfo{{URL: "wyze://1.2.3.4"}},
			},
			"backyard": {},
		})
	}))
	defer server.Close()

	c := NewAPIClient(server.URL, zerolog.Nop())
	streams, err := c.ListStreams(context.Background())
	if err != nil {
		t.Fatalf("ListStreams: %v", err)
	}
	if len(streams) != 2 {
		t.Errorf("streams = %d, want 2", len(streams))
	}
	if len(streams["front_door"].Producers) != 1 {
		t.Error("front_door should have 1 producer")
	}
}

func TestAPIClient_AddStream(t *testing.T) {
	var gotName, gotSrc string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("method = %q, want PUT", r.Method)
		}
		gotName = r.URL.Query().Get("name")
		gotSrc = r.URL.Query().Get("src")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewAPIClient(server.URL, zerolog.Nop())
	err := c.AddStream(context.Background(), "test_cam", "wyze://1.2.3.4?uid=X")
	if err != nil {
		t.Fatalf("AddStream: %v", err)
	}
	if gotName != "test_cam" {
		t.Errorf("name = %q", gotName)
	}
	if gotSrc != "wyze://1.2.3.4?uid=X" {
		t.Errorf("src = %q", gotSrc)
	}
}

func TestAPIClient_DeleteStream(t *testing.T) {
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewAPIClient(server.URL, zerolog.Nop())
	err := c.DeleteStream(context.Background(), "test_cam")
	if err != nil {
		t.Fatalf("DeleteStream: %v", err)
	}
	if gotMethod != "DELETE" {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
}

func TestAPIClient_HasActiveProducer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]*StreamInfo{
			"active_cam": {
				Producers: []ProducerInfo{{URL: "wyze://1.2.3.4"}},
			},
			"idle_cam": {},
		})
	}))
	defer server.Close()

	c := NewAPIClient(server.URL, zerolog.Nop())

	active, err := c.HasActiveProducer(context.Background(), "active_cam")
	if err != nil {
		t.Fatalf("HasActiveProducer: %v", err)
	}
	if !active {
		t.Error("active_cam should have active producer")
	}

	idle, err := c.HasActiveProducer(context.Background(), "idle_cam")
	if err != nil {
		t.Fatal(err)
	}
	if idle {
		t.Error("idle_cam should not have active producer")
	}

	missing, err := c.HasActiveProducer(context.Background(), "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if missing {
		t.Error("nonexistent should not have active producer")
	}
}

func TestAPIClient_GetSnapshot(t *testing.T) {
	jpegData := []byte{0xFF, 0xD8, 0xFF, 0xE0} // JPEG magic bytes
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("src") != "test_cam" {
			t.Errorf("src = %q", r.URL.Query().Get("src"))
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegData)
	}))
	defer server.Close()

	c := NewAPIClient(server.URL, zerolog.Nop())
	data, err := c.GetSnapshot(context.Background(), "test_cam")
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if len(data) != len(jpegData) {
		t.Errorf("data length = %d, want %d", len(data), len(jpegData))
	}
}

func TestAPIClient_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	c := NewAPIClient(server.URL, zerolog.Nop())

	_, err := c.ListStreams(context.Background())
	if err == nil {
		t.Error("expected error for 500 response")
	}

	err = c.AddStream(context.Background(), "x", "y")
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestStreamInfo_ConnectedAndReceivedBytes(t *testing.T) {
	const connectedJSON = `{
		"producers":[{
			"id":42,
			"format_name":"wyze",
			"protocol":"wyze/dtls",
			"remote_addr":"192.168.4.40:48511",
			"source":"wyze://192.168.4.40",
			"receivers":[{"id":43,"bytes":6273499,"packets":594},{"id":44,"bytes":1200,"packets":10}]
		}],
		"consumers":[{"id":36,"format_name":"rtsp","protocol":"rtsp+tcp","remote_addr":"192.168.4.175:49482"}]
	}`
	var connected StreamInfo
	if err := json.Unmarshal([]byte(connectedJSON), &connected); err != nil {
		t.Fatalf("decode connected: %v", err)
	}
	if len(connected.Producers) != 1 || !connected.Producers[0].Connected() {
		t.Fatal("connected producer should report Connected()")
	}
	if got := connected.ReceivedBytes(); got != 6273499+1200 {
		t.Fatalf("ReceivedBytes = %d, want %d", got, 6273499+1200)
	}

	const idleJSON = `{"producers":[{"url":"wyze://192.168.4.40"}]}`
	var idle StreamInfo
	if err := json.Unmarshal([]byte(idleJSON), &idle); err != nil {
		t.Fatalf("decode idle: %v", err)
	}
	if idle.Producers[0].Connected() {
		t.Fatal("url-only producer should not be Connected()")
	}
	if idle.ReceivedBytes() != 0 {
		t.Fatalf("idle ReceivedBytes = %d, want 0", idle.ReceivedBytes())
	}

	const webrtcJSON = `{
		"producers":[{
			"id":7,
			"url":"wss://wyze-mars-webcsrv.wyzecam.com",
			"receivers":[{"bytes":999,"packets":1}]
		}]
	}`
	var webrtc StreamInfo
	if err := json.Unmarshal([]byte(webrtcJSON), &webrtc); err != nil {
		t.Fatalf("decode webrtc: %v", err)
	}
	if !webrtc.Producers[0].Connected() {
		t.Fatal("webrtc producer with receivers should be Connected()")
	}
	if webrtc.ReceivedBytes() != 999 {
		t.Fatalf("webrtc ReceivedBytes = %d", webrtc.ReceivedBytes())
	}
}

func TestAPIClient_Preload(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		switch {
		case r.URL.Path == "/api/preload" && r.Method == "PUT":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/preload" && r.Method == "DELETE":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/preload" && r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]bool{"kjs_room": true})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := NewAPIClient(server.URL, zerolog.Nop())
	if err := c.AddPreload(context.Background(), "kjs_room"); err != nil {
		t.Fatalf("AddPreload: %v", err)
	}
	if gotMethod != "PUT" || gotPath != "/api/preload" {
		t.Fatalf("AddPreload method/path = %s %s", gotMethod, gotPath)
	}
	if gotQuery != "src=kjs_room&video&audio" {
		t.Fatalf("AddPreload query = %q", gotQuery)
	}

	if err := c.DeletePreload(context.Background(), "kjs_room"); err != nil {
		t.Fatalf("DeletePreload: %v", err)
	}
	set, err := c.ListPreloads(context.Background())
	if err != nil {
		t.Fatalf("ListPreloads: %v", err)
	}
	if !set["kjs_room"] {
		t.Fatal("expected kjs_room in preloads")
	}
}

func TestAPIClient_AddPreloadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("fail"))
	}))
	defer server.Close()

	c := NewAPIClient(server.URL, zerolog.Nop())
	if err := c.AddPreload(context.Background(), "x"); err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestAPIClient_ConnectionRefused(t *testing.T) {
	c := NewAPIClient("http://localhost:19999", zerolog.Nop())

	_, err := c.ListStreams(context.Background())
	if err == nil {
		t.Error("expected error for connection refused")
	}
}
