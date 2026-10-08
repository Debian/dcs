package web

import (
	"encoding/json"
	"log"
	"sync/atomic"
	"time"

	"github.com/Debian/dcs/internal/frequency"
)

// Since multiple users can perform the same query at (roughly) the same time
// (especially if a query gets viral on twitter), all messages that should be
// sent out to the user are stored in a slice.
//
// Each of the events has a sequence number, which corresponds to its position
// in the slice.
//
// Each client connection calls getEvent() to get the next event (blockingly).
//
// The three different cases (user who sends the query, user who sends the same
// query before the query is finished, user who requests a query which is
// already finished) are thus handled in exactly the same way.
//
// In order to preserve bandwidth, old events can be deleted, e.g. a new
// ProgressUpdate event deletes older ProgressUpdates, since only the very
// latest progress is interesting for clients that “join” in on the query.

type obsoletableEvent interface {
	ObsoletedBy(newEvent *obsoletableEvent) bool
	EventType() string
}

// An arbitrary event, such as a progress update, a search result or an error.
type event struct {
	data     []byte
	original obsoletableEvent
	obsolete *atomic.Bool
}

func (s *queryState) addEvent(data []byte, origdata any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	original, _ := origdata.(obsoletableEvent)
	s.events = append(s.events, event{
		data:     data,
		obsolete: new(atomic.Bool),
		original: original})
	// An empty message marks the query as finished, but further errors can
	// occur, so we store whether we’ve seen an empty message for use in
	// queryCompleted().
	if len(data) == 0 && !s.completed() {
		close(s.done)
		s.ended = time.Now()
		activeQueries.Sub(1)
		frequency.DecUsers()
	}
	s.newEvent.Broadcast()
}

// Like addEvent, but marshals data using encoding/json.
func (s *queryState) addEventMarshal(data any) {
	bytes, err := json.Marshal(data)
	if err != nil {
		log.Fatal(err)
	}

	s.addEvent(bytes, data)

	if original, ok := data.(obsoletableEvent); ok {
		s.mu.Lock()
		defer s.mu.Unlock()

		// We cannot obsolete events once the query is done, because then all
		// events before the done marker may get obsoleted (e.g. all progress
		// updates, for a query with 0 files).
		if s.completed() {
			return
		}

		// Consider all events before the just added event for obsoletion. At
		// most one event will be obsoleted.
		events := s.events
		for i := len(events) - 2; i >= 0; i-- {
			if events[i].original == nil {
				continue
			}
			if events[i].original.ObsoletedBy(&original) {
				events[i].obsolete.Store(true)
				break
			}
		}
	}
}

func (s *queryState) getEvent(lastseen int) (event, int, bool) {
	// We need to prevent new events being added, otherwise we could deadlock.
	s.mu.Lock()
	defer s.mu.Unlock()
	for !s.released && lastseen+1 >= len(s.events) {
		log.Printf("[%s] lastseen=%d, waiting\n", s.queryid, lastseen)
		s.newEvent.Wait()
	}
	if s.released {
		return event{}, 0, false
	}
	ev := s.events[lastseen+1]
	return ev, lastseen + 1, true
}

func (s *queryState) completed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}
