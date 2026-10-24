package frequency

import (
	"context"
	"flag"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"
)

var changeGovernor = flag.Bool("change_cpu_governor", false, "If enabled, DCS will run 'sudo cpupower frequency-set --governor performance' while queries are running. This has a measurable uplift on Intel CPUs.")

type frequencyManager struct {
	mu    sync.Mutex
	users int
}

func (fm *frequencyManager) setGovernor(g string) {
	if !*changeGovernor {
		return
	}

	// prevent indefinitely hanging processes
	ctx, canc := context.WithTimeout(context.Background(), 30*time.Second)
	defer canc()

	// Assumes a password-less sudo configuration like so:
	//
	// # Allow Debian Code Search to dynamically adjust the CPU frequency governor
	// dcs	ALL=(ALL:ALL) NOPASSWD:/usr/bin/cpupower frequency-set --governor performance
	// dcs	ALL=(ALL:ALL) NOPASSWD:/usr/bin/cpupower frequency-set --governor powersave
	cpupower := exec.CommandContext(ctx, "sudo", "cpupower", "frequency-set", "--governor", g)
	// Stdout intentionally not hooked up to prevent log spam
	cpupower.Stderr = os.Stderr
	if err := cpupower.Run(); err != nil {
		log.Printf("setting CPU frequency governor: %v", err)
		return
	}
	log.Printf("CPU frequency governor changed to %s", g)
}

func (fm *frequencyManager) decUsers() {
	fm.mu.Lock()
	if fm.users > 0 {
		fm.users--
	}
	fm.mu.Unlock()

	go func() {
		time.Sleep(1 * time.Minute)

		fm.mu.Lock()
		anyUsers := fm.users > 0
		fm.mu.Unlock()
		if anyUsers {
			return // some other release() call will check
		}
		fm.setGovernor("powersave")
	}()
}

func (fm *frequencyManager) incUsers() {
	fm.mu.Lock()
	fm.users++
	fm.mu.Unlock()

	fm.setGovernor("performance")
}

var fm = &frequencyManager{}

func IncUsers() { fm.incUsers() }
func DecUsers() { fm.decUsers() }
