package main

import (
	"flag"
	"github.com/Debian/dcs/rackspace"

	"log"
	"strings"
	"time"
)

var (
	rs     *rackspace.RackspaceClient
	dryrun = flag.Bool("dryrun",
		true,
		"Whether to actually delete servers/volumes or just print their IDs.")
)

func main() {
	flag.Parse()
	var err error
	rs, err = rackspace.NewClient()
	if err != nil {
		log.Fatal("Could not create new rackspace client: %v\n", err)
	}

	servers, err := rs.GetServers()
	if err != nil {
		log.Fatal(err)
	}

	// We look for servers called NEW-dcs-0 to figure out the newest.
	var (
		newestId       string
		newestCreation time.Time
	)

	for _, server := range servers {
		if server.Name == "NEW-dcs-0" &&
			server.Created().After(newestCreation) {
			newestId = server.Id
			newestCreation = server.Created()
		}
	}

	log.Printf("Newest NEW-dcs-0 server is %s (created at %v)\n",
		newestId, newestCreation)

	for _, server := range servers {
		if strings.HasPrefix(server.Name, "NEW-dcs-") &&
			server.Created().Before(newestCreation) {

			log.Printf("Deleting server %s (created %v, IPv4 %s, ID %s)\n",
				server.Name, server.Created(), server.AccessIPv4, server.Id)

			if !*dryrun {
				if err := rs.DeleteServer(server.Id); err != nil {
					log.Fatal(err)
				}
			}
		}
	}

	// TODO: wait until all servers have status == deleted

	// cleanup unused volumes
	volumes, err := rs.GetVolumes()
	if err != nil {
		log.Fatal(err)
	}

	for _, volume := range volumes {
		if strings.HasPrefix(volume.DisplayName, "NEW-dcs-") &&
			volume.Status() == "AVAILABLE" {
			log.Printf("Deleting unused codesearch volume %s\n", volume.Id)
			if !*dryrun {
				if err := rs.DeleteVolume(volume.Id); err != nil {
					log.Fatal(err)
				}
			}
		}
	}
}
