package camera

import "context"

func (m *Manager) wantsPreload(cam *Camera) bool {
	if cam == nil || m.cfg == nil || !m.cfg.PreloadWanted(cam.Name()) {
		return false
	}
	_, protocol := m.streamSourceFor(cam)
	return protocol != "gwell"
}

func (m *Manager) isPreloaded(name string) bool {
	return m.wantsPreload(m.GetCamera(name))
}

func (m *Manager) ensurePreloadAsync(ctx context.Context, cam *Camera) {
	if !m.wantsPreload(cam) {
		return
	}
	name := cam.Name()
	if _, loaded := m.preloadInFlight.LoadOrStore(name, struct{}{}); loaded {
		return
	}
	go func() {
		defer m.preloadInFlight.Delete(name)
		m.runAddPreload(ctx, cam)
	}()
}

func (m *Manager) runAddPreload(ctx context.Context, cam *Camera) {
	go2rtc := m.go2rtcClient()
	if go2rtc == nil {
		return
	}
	_, protocol := m.streamSourceFor(cam)
	m.preloadAPI.Lock()
	err := go2rtc.AddPreload(ctx, cam.Name())
	m.preloadAPI.Unlock()
	if err != nil {
		dec := m.stallTracker.RecordPreloadFailure(cam.Name(), protocol)
		if !dec.Restart {
			m.log.Warn().Err(err).
				Str("cam", cam.Name()).
				Str("protocol", protocol).
				Int("failures", dec.Failures).
				Msg("preload add failed")
			return
		}
		m.log.Warn().Err(err).
			Str("cam", cam.Name()).
			Str("protocol", protocol).
			Int("failures", dec.Failures).
			Msg("preload failed, restarting stream")
		m.restartStreamAsync(ctx, cam)
		return
	}
	m.stallTracker.RecordPreloadSuccess(cam.Name())
}

func (m *Manager) restartStreamAsync(ctx context.Context, cam *Camera) {
	if _, loaded := m.stallRestarting.LoadOrStore(cam.Name(), struct{}{}); loaded {
		return
	}
	go func(c *Camera) {
		defer m.stallRestarting.Delete(c.Name())
		if go2rtc := m.go2rtcClient(); go2rtc != nil {
			_ = go2rtc.DeleteStream(ctx, c.Name())
		}
		m.changeState(c, StateOffline)
		m.connectCamera(ctx, c)
	}(cam)
}
