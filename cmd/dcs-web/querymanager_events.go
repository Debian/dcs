package main

import (
	"encoding/json"
	"github.com/Debian/dcs/varz"
	"log"
	"time"
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
	obsolete *bool
}

func addEvent(queryid string, data []byte, origdata interface{}) {
	stateMu.Lock()
	defer stateMu.Unlock()
	s := state[queryid]
	original, _ := origdata.(obsoletableEvent)
	s.events = append(s.events, event{
		data:     data,
		obsolete: new(bool),
		original: original})
	// An empty message marks the query as finished, but further errors can
	// occur, so we store whether we’ve seen an empty message for use in
	// queryCompleted().
	if !s.done && len(data) == 0 {
		s.done = true
		s.ended = time.Now()
		varz.Decrement("active-queries")
	}
	state[queryid] = s

	state[queryid].newEvent.Broadcast()
}

// Like addEvent, but marshals data using encoding/json.
func addEventMarshal(queryid string, data interface{}) {
	bytes, err := json.Marshal(data)
	if err != nil {
		log.Fatal(err)
	}

	addEvent(queryid, bytes, data)

	if original, ok := data.(obsoletableEvent); ok {
		stateMu.Lock()
		defer stateMu.Unlock()

		// We cannot obsolete events once the query is done, because then all
		// events before the done marker may get obsoleted (e.g. all progress
		// updates, for a query with 0 files).
		if state[queryid].done {
			return
		}

		// Consider all events before the just added event for obsoletion. At
		// most one event will be obsoleted.
		events := state[queryid].events
		for i := len(events) - 2; i >= 0; i-- {
			if events[i].original == nil {
				continue
			}
			if events[i].original.ObsoletedBy(&original) {
				*(events[i].obsolete) = true
				break
			}
		}
	}
}

func getEvent(queryid string, lastseen int) (event, int) {
	// We need to prevent new events being added, otherwise we could deadlock.
	stateMu.Lock()
	s := state[queryid]
	if lastseen+1 < len(s.events) {
		stateMu.Unlock()
		return s.events[lastseen+1], lastseen + 1
	}
	state[queryid].newEvent.L.Lock()
	stateMu.Unlock()
	for lastseen+1 >= len(state[queryid].events) {
		log.Printf("[%s] lastseen=%d, waiting\n", queryid, lastseen)
		state[queryid].newEvent.Wait()
	}
	state[queryid].newEvent.L.Unlock()
	return state[queryid].events[lastseen+1], lastseen + 1
}

func queryCompleted(queryid string) bool {
	return state[queryid].done
}
