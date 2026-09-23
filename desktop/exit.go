package main

import "sync"

// exitState 将窗口隐藏与真正退出分开；前端握手前暂存退出请求。
type exitState struct {
	mu                                        sync.Mutex
	frontendReady, pending, notified, allowed bool
}

func (s *exitState) request(hasCore bool) (allow, notify bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.allowed || !hasCore {
		return true, false
	}
	s.pending = true
	if s.frontendReady && !s.notified {
		s.notified = true
		return false, true
	}
	return false, false
}

func (s *exitState) ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frontendReady = true
	if s.pending && !s.notified {
		s.notified = true
		return true
	}
	return false
}

func (s *exitState) cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending, s.notified = false, false
}

func (s *exitState) confirm() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allowed = true
}

func (s *exitState) hideOnClose(trayAvailable bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return trayAvailable && !s.pending && !s.allowed
}
