// Sets up or tears down a batch DCS stack using systemd.
//
// Note that you must never start multiple copies of this binary at
// the same time, otherwise createStack() can suffer from race
// conditions and you might end up with more stacks than -max_stacks.
// This requirement is currently satisfied because only the non-batch
// dcs-web starts this binary, but in case that every changes, it
// would be better to make a service out of this code.
package main

// TODO: it would be better if we would use dbus directly to talk to
// systemd, but I never worked with dbus in go, so let’s do that at a
// later point. Maybe a good opportunity to write a systemd go
// package?

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var (
	maxStacks = flag.Int("max_stacks", 5,
		"Maximum number of batch stacks running at the same time")
	unitDir = flag.String("unit_dir",
		"/run/systemd/system/",
		"Directory to store the systemd unit files in.")
	garbageCollectionHours = flag.Int("garbage_collection_hours",
		2,
		"Maximum amount of hours a batch stack is allowed to run")
)

const (
	batchPrefixFmt = "dcs-batch%d"
)

// Returns the globbed dcs-batch*-source-backend.service file names
// for the currently existing stacks.
func currentStackNames() []string {
	pattern := filepath.Join(*unitDir, "dcs-batch*-source-backend.service")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		log.Fatal(err)
	}

	return matches
}

func systemctlStartForStackOrDie(stackId int, suffix string) {
	cmd := exec.Command("systemctl", "start", fmt.Sprintf(batchPrefixFmt+suffix, stackId))
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func createStack() int {
	// Get the next free batch stack id
	names := currentStackNames()
	sort.Strings(names)
	stackId := 0
	for _, name := range names {
		if strings.HasSuffix(name, fmt.Sprintf(batchPrefixFmt+"-source-backend.service", stackId)) {
			stackId++
		}
	}

	// The listen port schema is 30xyz, with:
	// x = stack ID
	// y = backend, 0 = source, 1 = index, 2 = dcs-web
	// z = backend instance, always 0 except for index backends.
	contents := []byte(fmt.Sprintf(`
.include /lib/systemd/system/dcs-common.service
.include /lib/systemd/system/dcs-batch-common.service

[Unit]
Description=DCS batch stack source-backend

[Service]
ExecStart=/usr/bin/source-backend \
    -unpacked_path=/dcs/unpacked/ \
    -listen_address=localhost:30%d00
`, stackId))
	if err := ioutil.WriteFile(filepath.Join(*unitDir,
		fmt.Sprintf(batchPrefixFmt+"-source-backend.service", stackId)),
		contents, 0644); err != nil {
		log.Fatal(err)
	}

	contents = []byte(fmt.Sprintf(`
.include /lib/systemd/system/dcs-common.service
.include /lib/systemd/system/dcs-batch-common.service

[Unit]
Description=DCS batch stack index-backend

[Service]
ExecStart=/usr/bin/index-backend \
    -index_path=/dcs/index.%%i.idx \
    -listen_address=localhost:30%d1%%i
`, stackId))
	if err := ioutil.WriteFile(filepath.Join(*unitDir,
		fmt.Sprintf(batchPrefixFmt+"-index-backend@.service", stackId)),
		contents, 0644); err != nil {
		log.Fatal(err)
	}

	// TODO: make dcs-web.service socket-activated for
	// performance. Then we don’t have to delay connecting to the
	// service to send the batch query (because dcs-web might take
	// a bit to initialize).

	// TODO: the need to mention all index backends explicitly is
	// unfortunate. Ideally, we would auto-discover them in
	// dcs-web. See also https://github.com/Debian/dcs/issues/23
	contents = []byte(fmt.Sprintf(`
.include /lib/systemd/system/dcs-common.service
.include /lib/systemd/system/dcs-batch-common.service

[Unit]
Description=DCS batch stack dcs-web

[Service]
ExecStart=/usr/bin/dcs-web \
    -template_pattern=/usr/share/dcs/templates/* \
    -index_backends=localhost:30%d10,localhost:30%d11,localhost:30%d12,localhost:30%d13,localhost:30%d14,localhost:30%d15 \
    -listen_address=localhost:30%d20
`, stackId, stackId, stackId, stackId, stackId, stackId, stackId))
	if err := ioutil.WriteFile(filepath.Join(*unitDir,
		fmt.Sprintf(batchPrefixFmt+"-dcs-web.service", stackId)),
		contents, 0644); err != nil {
		log.Fatal(err)
	}

	// Use daemon-reload so that systemd picks up the new unit
	// files.
	cmd := exec.Command("systemctl", "daemon-reload")
	if err := cmd.Run(); err != nil {
		log.Fatalf("systemctl daemon-reload: %v", err)
	}

	systemctlStartForStackOrDie(stackId, "-source-backend.service")
	for i := 0; i < 6; i++ {
		systemctlStartForStackOrDie(stackId, fmt.Sprintf("-index-backend@%d.service", i))
	}
	systemctlStartForStackOrDie(stackId, "-dcs-web.service")

	return stackId
}

func deleteFileForStackOrDie(stackId int, suffix string) {
	path := filepath.Join(*unitDir, fmt.Sprintf(batchPrefixFmt+suffix, stackId))
	if err := os.Remove(path); err != nil {
		log.Fatal(err)
	}
}

func destroyStack(stackId int) {
	unitOutput, err := exec.Command("systemctl", "list-units", "--full").Output()
	if err != nil {
		log.Fatalf("systemctl list-units --full: %v", err)
	}

	stackPrefix := fmt.Sprintf(batchPrefixFmt, stackId)
	for _, line := range strings.Split(string(unitOutput), "\n") {
		if !strings.HasPrefix(line, stackPrefix) {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			log.Fatal("Invalid systemctl list-units output line: %s\n", line)
		}
		service := fields[0]
		log.Printf("Stopping service %s\n", service)
		cmd := exec.Command("systemctl", "stop", service)
		if err := cmd.Run(); err != nil {
			// XXX: We could use systemctl kill as a fall
			// back, but let’s try to be nice first.
			log.Fatalf("systemctl stop %s: %v", service, err)
		}

		// Use reset-failed because the unit enters failed
		// state after using systemctl stop. I am not entirely
		// sure if that is intended behavior on systemd’s
		// side.
		cmd = exec.Command("systemctl", "reset-failed", service)
		if err := cmd.Run(); err != nil {
			log.Fatalf("systemctl reset-failed %s: %v", service, err)
		}
	}

	deleteFileForStackOrDie(stackId, "-source-backend.service")
	deleteFileForStackOrDie(stackId, "-index-backend@.service")
	deleteFileForStackOrDie(stackId, "-dcs-web.service")

	// Use daemon-reload so that systemd realizes the unit files
	// we just deleted disappeared.
	cmd := exec.Command("systemctl", "daemon-reload")
	if err := cmd.Run(); err != nil {
		log.Fatalf("systemctl daemon-reload: %v", err)
	}
}

// Kills stacks that have been running for more than -garbage_collection_hours.
func garbageCollectStacks() {
	// TODO: use
	// systemctl show -p ActiveEnterTimestampMonotonic dcs-batch0-source-backend.service
	// ActiveEnterTimestampMonotonic=10314873549658
}

func main() {
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		log.Fatal("Syntax: dcs-batch-helper <action>")
	}

	switch args[0] {
	case "create":
		if len(currentStackNames()) > *maxStacks {
			log.Fatal("Too many batch stacks are currently running. Please try again later.")
		}

		fmt.Printf("%d\n", createStack())
	case "destroy":
		if len(args) != 2 {
			log.Fatal("Syntax: dcs-batch-helper destroy <stack-id>")
		}
		stackId, err := strconv.Atoi(args[1])
		if err != nil {
			log.Fatalf("Invalid <stack-id>: %v", err)
		}

		destroyStack(stackId)
	case "garbagecollect":
		garbageCollectStacks()
	}
}
