package server

import (
	"net/http"
	"sync"
	"time"

	"github.com/lijuno/agent-pet/internal/flavor"
	"github.com/lijuno/agent-pet/internal/update"
)

// The update endpoint is where petd learns that a newer version exists.
//
// It learns it from petctl, which does the network call, and holds it only to
// display it. Nothing in petd goes looking: the package that would do the
// looking is not linked into this program (ADR 0008), and the state below is
// the whole of what the daemon knows about the outside world.
//
// Anything running as this user can post here, as with every other route, and
// what is posted ends up in a menu-bar title. So it is validated as untrusted
// input, not as a message from a component we wrote — see update.Status.
type updateState struct {
	mu   sync.Mutex
	last update.Status
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.updateStatus())
	case http.MethodPost:
		s.postUpdate(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) postUpdate(w http.ResponseWriter, r *http.Request) {
	var st update.Status
	if err := decode(r, &st); err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := st.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	// The channel is a fact about this build, so a result for the other one is
	// refused rather than stored. There is no way for it to become true later:
	// a build cannot change channel, only be replaced by the other app.
	if st.Channel != "" && st.Channel != s.channel() {
		badRequest(w, "this is the "+string(s.channel())+" build; it cannot report a "+string(st.Channel)+" version")
		return
	}
	st.Channel = s.channel()
	st.Current = Version
	if st.CheckedAt.IsZero() {
		st.CheckedAt = time.Now()
	}
	s.upd.mu.Lock()
	s.upd.last = st
	s.upd.mu.Unlock()

	if s.OnUpdate != nil {
		s.OnUpdate(s.updateStatus())
	}
	writeJSON(w, http.StatusOK, s.updateStatus())
}

// updateStatus is the current answer. Current and Channel are always this
// build's own: what the caller claimed about either is not evidence.
func (s *Server) updateStatus() update.Status {
	s.upd.mu.Lock()
	last := s.upd.last
	s.upd.mu.Unlock()

	last.Channel = s.channel()
	last.Current = Version
	return last
}

// channel is fixed at build time. See internal/flavor.
func (s *Server) channel() update.Channel { return flavor.Current().Channel }
