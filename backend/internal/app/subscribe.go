package app

// Head notifications let a presentation layer (the SSE progress stream) wake
// when a Branch's Head_Tick moves. They carry no data and never advance
// anything; a subscriber re-reads the head after each wake-up.

// Subscribe returns a channel that receives a value (coalesced, capacity 1)
// after each Advance of the Branch, and a cancel function that must be called
// to release the subscription. Cancel is idempotent.
func (s *BranchService) Subscribe(id BranchID) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.subMu.Lock()
	if s.subs == nil {
		s.subs = map[BranchID]map[chan struct{}]struct{}{}
	}
	if s.subs[id] == nil {
		s.subs[id] = map[chan struct{}]struct{}{}
	}
	s.subs[id][ch] = struct{}{}
	s.subMu.Unlock()
	return ch, func() {
		s.subMu.Lock()
		defer s.subMu.Unlock()
		if set := s.subs[id]; set != nil {
			delete(set, ch)
			if len(set) == 0 {
				delete(s.subs, id)
			}
		}
	}
}

// notifyHead wakes every subscriber of the Branch without blocking.
func (s *BranchService) notifyHead(id BranchID) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for ch := range s.subs[id] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
