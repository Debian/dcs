// Listens for new uploads on the Debian FedMsg bus, see https://wiki.debian.org/FedMsg
// Once a new upload appears, tells dcs-feeder to look for that package and
// feed it to the appropriate dcs-package-importer instance.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/pebbe/zmq4"
)

var (
	feeder = flag.String("feeder",
		"localhost:21020",
		"host:port where dcs-feeder is running")
)

type uploadMsg struct {
	Msg uploadDetails
}

type uploadDetails struct {
	Distribution string
	Source       string
	Version      string
}

func main() {
	subscriber, _ := zmq4.NewSocket(zmq4.SUB)
	defer subscriber.Close()
	subscriber.SetSubscribe("org.debian.dev.debmessenger.package.upload")
	subscriber.Connect("tcp://fedmsg.debian.net:9940")
	log.Printf("Connected, listening for uploads...\n")
	var m uploadMsg
	for {
		// A msg consists of topic and JSON-encoded contents.
		msg, err := subscriber.RecvMessageBytes(0)
		if err != nil {
			// We don’t treat these as fatal because we get errors for
			// interrupted syscalls and such.
			log.Printf("Error in RecvMessageBytes(): %v\n", err)
			continue
		}
		if len(msg) != 2 {
			log.Fatalf("Malformed message received. Expected 2 elements, got %d elements\n", len(msg))
		}
		json.Unmarshal(msg[1], &m)
		log.Printf("Source %q (version %q) was uploaded to %q\n", m.Msg.Source, m.Msg.Version, m.Msg.Distribution)
		if m.Msg.Distribution != "unstable" {
			continue
		}
		if strings.HasSuffix(m.Msg.Source, "-data") {
			log.Printf("Skipping %q because it ends in -data.\n", m.Msg.Source)
			continue
		}

		dscName := fmt.Sprintf("%s_%s.dsc", m.Msg.Source, m.Msg.Version)
		resp, err := http.PostForm(fmt.Sprintf("http://%s/lookfor", *feeder), url.Values{"file": {dscName}})
		resp.Body.Close()
		if err != nil {
			log.Printf("Could not tell feeder to look for %q: %v\n", dscName, err)
			continue
		}
		if resp.StatusCode != 200 {
			log.Printf("Could not tell feeder to look for %q: %+v\n", dscName, resp)
		}
	}
}
