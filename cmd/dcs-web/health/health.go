// vim:ts=4:sw=4:noexpandtab

// Health checking for sources.debian.net (and potentially other services in
// the future), so that we can reliably redirect to the service when it is
// available and fall back to our own /show if not.
package health

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"time"
)

var status = make(chan healthRequest)

type healthRequest struct {
	service  string
	response chan bool
}

type healthUpdate struct {
	service string
	healthy bool
}

func periodically(checkFunc func() healthUpdate, updates chan healthUpdate) {
	for {
		updates <- checkFunc()
		time.Sleep(30 * time.Second)
	}
}

// health-checks sources.debian.net, run within a goroutine
func checkSDN() (update healthUpdate) {
	update.service = "sources.debian.net"

	update.healthy = false
	client := &http.Client{
		Transport: &http.Transport{
			// Dials a network address with a connection timeout of 5 seconds and a data
			// deadline of 5 seconds.
			Dial: func(netw, addr string) (net.Conn, error) {
				conn, err := net.DialTimeout(netw, addr, 5*time.Second)
				if err != nil {
					return nil, err
				}
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				return conn, nil
			},
		},
	}
	responseChan := make(chan *http.Response)
	go func() {
		resp, _ := client.Get("http://sources.debian.net/api/ping/")
		responseChan <- resp
	}()
	select {
	case <-time.After(15 * time.Second):
		// TODO: if this never ever happens we can make this code simpler and blockingly call client.Get()
		log.Printf("BUG BUG BUG: The http client.Get took too long even though it is supposed to have a timeout.")
		return
	case resp := <-responseChan:
		if resp == nil {
			log.Printf("health check: sources.debian.net did not answer to HTTP\n")
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			log.Printf("health check: sources.debian.net returned code %d\n", resp.StatusCode)
			return
		}
		type sdnStatus struct {
			Status string
		}
		status := sdnStatus{}
		decoder := json.NewDecoder(resp.Body)
		if err := decoder.Decode(&status); err != nil {
			log.Printf("health check: sources.debian.net returned invalid JSON: %v\n", err)
			return
		}
		if status.Status != "ok" {
			log.Printf("health check: sources.debian.net returned status == false\n")
			return
		}
		update.healthy = true
	}
	return
}

func IsHealthy(service string) bool {
	response := make(chan bool)
	request := healthRequest{
		service:  service,
		response: response}
	status <- request
	return <-response
}

// Internally, this just starts a go routine per service that should be health-checked.
func StartChecking() {
	updates := make(chan healthUpdate)

	go periodically(checkSDN, updates)

	// Take updates and respond to health status requests in a single
	// goroutine. It is not safe to write/read to a map from multiple go
	// routines at the same time.
	go func() {
		health := make(map[string]bool)

		for {
			select {
			case update := <-updates:
				health[update.service] = update.healthy
			case request := <-status:
				request.response <- health[request.service]
			}
		}
	}()
}
