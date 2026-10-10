package config

import "testing"

func TestParsePreloadCameras(t *testing.T) {
	all, names := parsePreloadCameras("")
	if all || names != nil {
		t.Fatalf("empty: all=%v names=%v", all, names)
	}

	all, names = parsePreloadCameras("all")
	if !all || names != nil {
		t.Fatalf("all: all=%v names=%v", all, names)
	}
	all, _ = parsePreloadCameras(" ALL ")
	if !all {
		t.Fatal("ALL should enable preload all")
	}

	all, names = parsePreloadCameras(" kjs_room , isaiahs_room_cam ")
	if all || len(names) != 2 {
		t.Fatalf("list: all=%v names=%v", all, names)
	}
	if _, ok := names["kjs_room"]; !ok {
		t.Fatal("missing kjs_room")
	}
	if _, ok := names["isaiahs_room_cam"]; !ok {
		t.Fatal("missing isaiahs_room_cam")
	}
}

func TestPreloadWanted(t *testing.T) {
	cfg := &Config{PreloadCameras: map[string]struct{}{"kjs_room": {}}}
	if !cfg.PreloadWanted("kjs_room") || cfg.PreloadWanted("other") {
		t.Fatal("explicit list mismatch")
	}
	cfg = &Config{PreloadAll: true}
	if !cfg.PreloadWanted("anything") {
		t.Fatal("preload all")
	}
	cfg = &Config{}
	if cfg.PreloadWanted("kjs_room") {
		t.Fatal("default off")
	}
}

func TestLoadPreloadCamerasEnv(t *testing.T) {
	t.Setenv("WYZE_EMAIL", "")
	t.Setenv("WYZE_PASSWORD", "")
	t.Setenv("WYZE_API_ID", "")
	t.Setenv("WYZE_API_KEY", "")
	t.Setenv("PRELOAD_CAMERAS", "kjs_room, living_room_cam")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PreloadAll {
		t.Fatal("should not be all")
	}
	if !cfg.PreloadWanted("kjs_room") || !cfg.PreloadWanted("living_room_cam") {
		t.Fatal("wanted cams from env")
	}
}
