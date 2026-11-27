package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// Progress stream defaults and limits (security steering: 4 SSE connections
// per client IP; Req 27).
const (
	DefaultSSEHeartbeat    = 15 * time.Second
	DefaultSSEPollInterval = 250 * time.Millisecond
	MaxSSEConnsPerIP       = 4
)

// tickEventDTO is the data of a `tick` event: the TickFrame wire object of
// /tick-frames plus the engine warnings of the branch's world, when any.
type tickEventDTO struct {
	tickFrameDTO
	Warnings []warningDTO `json:"warnings,omitempty"`
}

// decisionWindowEventDTO is the data of a `decision_window` event.
type decisionWindowEventDTO struct {
	Tick    int64    `json:"tick"`
	Reasons []string `json:"reasons"`
}

// completedEventDTO is the data of a `completed` event.
type completedEventDTO struct {
	Tick       int64  `json:"tick"`
	StateHash  string `json:"stateHash"`
	Currency   string `json:"currency"`
	Disclaimer string `json:"disclaimer"`
}

// acquireSSE takes one stream slot for ip. It reports false at the cap.
func (s *Server) acquireSSE(ip string) bool {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	if s.sseConns[ip] >= MaxSSEConnsPerIP {
		return false
	}
	s.sseConns[ip]++
	return true
}

func (s *Server) releaseSSE(ip string) {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	if s.sseConns[ip] <= 1 {
		delete(s.sseConns, ip)
		return
	}
	s.sseConns[ip]--
}

// startTick reads the first tick to stream: Last-Event-ID + 1 on a
// reconnect, else the `fromTick` query, else 0.
func startTick(r *http.Request) (sim.Tick, error) {
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			return 0, badRequest("malformed header", issue("Last-Event-ID", "must be a tick number"))
		}
		return sim.Tick(n + 1), nil
	}
	n, present, err := queryInt(r, "fromTick", false)
	if err != nil {
		return 0, err
	}
	if present && n < 0 {
		return 0, newProblem("invalid-tick", "ticks are not negative")
	}
	return sim.Tick(n), nil
}

// progressStream serves GET /branches/{branchId}/progress. It only presents
// frames that are already computed; it never advances the simulation.
func (s *Server) progressStream(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "branchId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	bid := app.BranchID(id)
	next, err := startTick(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	b, err := s.svc.GetBranch(r.Context(), bid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	_, sc, err := s.svc.GetWorld(r.Context(), b.WorldID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	horizon := sim.Tick(sc.HorizonTicks)
	if next > horizon+1 {
		s.fail(w, r, newProblem("invalid-tick", "the start tick is beyond the scenario horizon"))
		return
	}
	warn, err := s.branchWarnings(r, bid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		s.fail(w, r, fmt.Errorf("response writer cannot flush"))
		return
	}
	ip := clientIP(r)
	if !s.acquireSSE(ip) {
		w.Header().Set("Retry-After", "1")
		writeProblem(w, r, newProblem("rate-limited", fmt.Sprintf("at most %d progress streams are open per address", MaxSSEConnsPerIP)))
		return
	}
	defer s.releaseSSE(ip)

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // the server write timeout must not cut a long stream
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	if err := rc.Flush(); err != nil {
		return
	}

	notify, cancel := s.svc.Subscribe(bid)
	defer cancel()
	poll := time.NewTicker(s.ssePoll)
	defer poll.Stop()
	beat := time.NewTimer(s.sseHeartbeat)
	defer beat.Stop()

	ctx := r.Context()
	send := func(tick sim.Tick, event string, data any) bool {
		raw, err := json.Marshal(data)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", tick, event, raw); err != nil {
			return false
		}
		if rc.Flush() != nil {
			return false
		}
		if !beat.Stop() {
			select {
			case <-beat.C:
			default:
			}
		}
		beat.Reset(s.sseHeartbeat)
		return true
	}

	for {
		if !s.drain(ctx, bid, &next, horizon, warn, send) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-notify:
		case <-poll.C:
		case <-beat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil || rc.Flush() != nil {
				return
			}
			beat.Reset(s.sseHeartbeat)
		}
	}
}

// drain emits every computed frame from *next up to the head and the
// horizon, then `completed` once the horizon frame is out. It returns false
// when the stream must end (write failure, cancelled context, read error).
func (s *Server) drain(ctx context.Context, bid app.BranchID, next *sim.Tick, horizon sim.Tick, warn []warningDTO,
	send func(sim.Tick, string, any) bool) bool {
	head, err := s.svc.HeadTick(ctx, bid)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Error("progress stream", "branch", string(bid), "err", err.Error())
		}
		return false
	}
	for *next <= head && *next <= horizon {
		f, err := s.svc.FrameAt(ctx, bid, *next)
		if err != nil {
			if ctx.Err() == nil {
				s.log.Error("progress stream", "branch", string(bid), "err", err.Error())
			}
			return false
		}
		dto := frameToDTO(f)
		if !send(*next, "tick", tickEventDTO{tickFrameDTO: dto, Warnings: warn}) {
			return false
		}
		var reasons []string
		for _, m := range dto.Markers {
			if m.Kind == "decision_window" {
				reasons = append(reasons, m.WindowReason)
			}
		}
		if len(reasons) > 0 && !send(*next, "decision_window", decisionWindowEventDTO{Tick: int64(*next), Reasons: reasons}) {
			return false
		}
		*next++
	}
	if *next > horizon && head >= horizon {
		// The horizon frame is out (or the client already had it): end the
		// stream with the final tick and hash. Also answers a reconnect that
		// presents Last-Event-ID = horizon.
		f, err := s.svc.FrameAt(ctx, bid, horizon)
		if err != nil {
			return false
		}
		send(horizon, "completed", completedEventDTO{
			Tick: int64(horizon), StateHash: hashHex(f.Hash), Currency: Currency, Disclaimer: sim.Disclaimer,
		})
		return false
	}
	return true
}
