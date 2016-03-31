package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	unpackedPath = flag.String("unpacked_path",
		"/tmp/dcs-hacking",
		"Path to the unpacked sources")
	localdcsPath = flag.String("localdcs_path",
		"~/.config/dcs-localdcs",
		"Directory in which to keep state for dcs-localdcs (TLS certificates, PID files, etc.)")
	stop = flag.Bool("stop",
		false,
		"Whether to stop the currently running localdcs instead of starting a new one")
	listenPackageImporter = flag.String("listen_package_importer",
		"localhost:21010",
		"listen address ([host]:port) for dcs-package-importer")
	listenIndexBackend = flag.String("listen_index_backend",
		"localhost:28081",
		"listen address ([host]:port) for dcs-index-backend")
	listenSourceBackend = flag.String("listen_source_backend",
		"localhost:28082",
		"listen address ([host]:port) for dcs-source-backend")
	listenWeb = flag.String("listen_web",
		"localhost:28080",
		"listen address ([host]:port) for dcs-web")
)

func installBinaries() error {
	cmd := exec.Command("go", "install", "github.com/Debian/dcs/cmd/...")
	cmd.Stderr = os.Stderr
	log.Printf("Compiling and installing binaries: %v\n", cmd.Args)
	return cmd.Run()
}

func help(binary string) error {
	err := exec.Command(binary, "-help").Run()
	if exiterr, ok := err.(*exec.ExitError); ok {
		status, ok := exiterr.Sys().(syscall.WaitStatus)
		if !ok {
			log.Panicf("cannot run on this platform: exec.ExitError.Sys() does not return syscall.WaitStatus")
		}
		// -help results in exit status 2, so that’s expected.
		if status.ExitStatus() == 2 {
			return nil
		}
	}
	return err
}

func verifyBinariesAreExecutable() error {
	binaries := []string{
		"dcs-package-importer",
		"dcs-index-backend",
		"dcs-source-backend",
		"dcs-web",
		"dcs-compute-ranking",
	}
	log.Printf("Verifying binaries are executable: %v\n", binaries)
	for _, binary := range binaries {
		if err := help(binary); err != nil {
			return err
		}
	}
	return nil
}

func compileStaticAssets() error {
	cmd := exec.Command("make")
	cmd.Stderr = os.Stderr
	cmd.Dir = "static"
	log.Printf("Compiling static assets: %v\n", cmd.Args)
	return cmd.Run()
}

// recordResource appends a line to a file in -localnet_dir so that we can
// clean up resources (tempdirs, pids) when being called with -stop later.
func recordResource(rtype, value string) error {
	f, err := os.OpenFile(filepath.Join(*localdcsPath, rtype+"s"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s\n", value)
	return err
}

func kill() error {
	pidsFile := filepath.Join(*localdcsPath, "pids")
	if _, err := os.Stat(pidsFile); os.IsNotExist(err) {
		return fmt.Errorf("-stop specified, but no localdcs instance found in -localdcs_path=%q", *localdcsPath)
	}

	pidsBytes, err := ioutil.ReadFile(pidsFile)
	if err != nil {
		return fmt.Errorf("Could not read %q: %v", pidsFile, err)
	}
	pids := strings.Split(string(pidsBytes), "\n")
	for _, pidline := range pids {
		if pidline == "" {
			continue
		}
		pid, err := strconv.Atoi(pidline)
		if err != nil {
			return fmt.Errorf("Invalid line in %q: %v", pidsFile, err)
		}

		process, err := os.FindProcess(pid)
		if err != nil {
			log.Printf("Could not find process %d: %v", pid, err)
			continue
		}
		if err := process.Kill(); err != nil {
			log.Printf("Could not kill process %d: %v", pid, err)
		}
	}

	os.Remove(pidsFile)

	return nil
}

func launchInBackground(binary string, args ...string) error {
	// TODO: redirect stderr into a file
	cmd := exec.Command(binary, args...)
	cmd.Stderr = os.Stderr
	// Put the robustirc servers into a separate process group, so that they
	// survive when robustirc-localnet terminates.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Could not start %q: %v", binary, err)
	}
	if err := recordResource("pid", strconv.Itoa(cmd.Process.Pid)); err != nil {
		return fmt.Errorf("Could not record pid of %q: %v", binary, err)
	}
	return nil
}

func httpReachable(addr string) error {
	// Poll the configured listening port to see if the server started up successfully.
	running := false
	var err error
	for try := 0; !running && try < 20; try++ {
		_, err = http.Get("http://" + addr)
		if err != nil {
			time.Sleep(250 * time.Millisecond)
			continue
		}

		// Any HTTP response is okay.
		return nil
	}
	return err
}

func feed(pkg, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	url := fmt.Sprintf("http://%s/import/%s/%s", *listenPackageImporter, pkg, filepath.Base(file))
	request, err := http.NewRequest("PUT", url, f)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Unexpected HTTP status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	return nil
}

func importTestdata() error {
	testdataFiles := make(map[string][]string)
	if err := filepath.Walk("testdata/pool", func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		dir := filepath.Dir(path)
		testdataFiles[dir] = append(testdataFiles[dir], path)
		return nil
	}); err != nil {
		return err
	}
	// e.g. testdataFiles = map[
	//   testdata/pool/main/i/i3-wm:[
	//     testdata/pool/main/i/i3-wm/i3-wm_4.5.1-2.debian.tar.gz
	//     testdata/pool/main/i/i3-wm/i3-wm_4.5.1-2.dsc
	//     testdata/pool/main/i/i3-wm/i3-wm_4.5.1.orig.tar.bz2]
	//   testdata/pool/main/z/zsh:[
	//     testdata/pool/main/z/zsh/zsh_5.2-3.debian.tar.xz
	//     testdata/pool/main/z/zsh/zsh_5.2-3.dsc
	//     testdata/pool/main/z/zsh/zsh_5.2.orig.tar.xz]]
	numPackages := 0
	for _, files := range testdataFiles {
		var dsc string
		var rest []string
		for _, file := range files {
			if filepath.Ext(file) == ".dsc" {
				dsc = file
			} else {
				rest = append(rest, file)
			}
		}
		if dsc == "" {
			continue
		}
		numPackages++
		// e.g.:
		// dsc "testdata/pool/main/i/i3-wm/i3-wm_4.5.1-2.dsc"
		// rest [
		//   testdata/pool/main/i/i3-wm/i3-wm_4.5.1-2.debian.tar.gz
		//   testdata/pool/main/i/i3-wm/i3-wm_4.5.1.orig.tar.bz2]
		pkg := filepath.Base(dsc)
		pkg = pkg[:len(pkg)-len(filepath.Ext(pkg))]
		log.Printf("Importing package %q (files %v, dsc %s)\n", pkg, rest, dsc)
		for _, file := range append(rest, dsc) {
			if err := feed(pkg, file); err != nil {
				return err
			}
		}
	}

	// Wait with merging until all packages were imported, but up to 5s max.
	for try := 0; try < 20; try++ {
		resp, err := http.Get("http://" + *listenPackageImporter + "/metrics")
		if err != nil {
			time.Sleep(250 * time.Millisecond)
			continue
		}
		body, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if strings.Contains(string(body), fmt.Sprintf("package_indexes_successful %d", numPackages)) {
			break
		}
	}

	_, err := http.Get("http://" + *listenPackageImporter + "/merge")
	return err
}

func main() {
	flag.Parse()

	if len(*localdcsPath) >= 2 && (*localdcsPath)[:2] == "~/" {
		usr, err := user.Current()
		if err != nil {
			log.Fatalf("Cannot expand -localdcs_path: %v", err)
		}
		*localdcsPath = strings.Replace(*localdcsPath, "~/", usr.HomeDir+"/", 1)
	}

	if err := os.MkdirAll(*localdcsPath, 0700); err != nil {
		log.Fatalf("Could not create directory %q for dcs-localdcs state: %v", *localdcsPath, err)
	}

	if *stop {
		if err := kill(); err != nil {
			log.Fatalf("Could not stop localdcs: %v", err)
		}
		return
	}

	if _, err := os.Stat(filepath.Join(*localdcsPath, "pids")); !os.IsNotExist(err) {
		log.Fatalf("There already is a localdcs instance running. Either use -stop or specify a different -localdcs_path")
	}

	if err := os.MkdirAll(*unpackedPath, 0755); err != nil {
		log.Fatalf("Could not create directory %q for unpacked files/index: %v", *unpackedPath, err)
	}

	if err := installBinaries(); err != nil {
		log.Fatalf("Compiling and installing binaries failed: %v", err)
	}

	if err := verifyBinariesAreExecutable(); err != nil {
		log.Fatalf("Could not find all required binaries: %v", err)
	}

	if err := compileStaticAssets(); err != nil {
		log.Fatalf("Compiling static assets failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(*localdcsPath, "key.pem")); os.IsNotExist(err) {
		log.Printf("Generating TLS certificate\n")
		if err := generatecert(*localdcsPath); err != nil {
			log.Fatalf("Could not generate TLS certificate: %v", err)
		}
	}

	rankingPath := filepath.Join(*localdcsPath, "ranking.json")
	if stat, err := os.Stat(rankingPath); err != nil || time.Since(stat.ModTime()) > 7*24*time.Hour {
		log.Printf("Computing ranking data\n")
		cmd := exec.Command(
			"dcs-compute-ranking",
			"-output_path="+rankingPath)
		cmd.Env = append(os.Environ(),
			"TMPDIR="+*localdcsPath)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("Could not compute ranking data: %v", err)
		}
	} else {
		log.Printf("Recent-enough rankings file %q found, not re-generating (delete to force)\n", rankingPath)
	}

	// Start package importer and import testdata/
	if err := launchInBackground(
		"dcs-package-importer",
		"-varz_avail_fs=",
		"-unpacked_path="+*unpackedPath,
		"-listen_address="+*listenPackageImporter); err != nil {
		log.Fatal(err.Error())
	}
	if err := httpReachable(*listenPackageImporter); err != nil {
		log.Fatalf("dcs-package-importer not reachable: %v", err)
	}
	if err := importTestdata(); err != nil {
		log.Fatalf("Could not import testdata/: %v", err)
	}

	if err := launchInBackground(
		"dcs-index-backend",
		"-varz_avail_fs=",
		"-index_path="+*unpackedPath+"/full.idx",
		"-tls_cert_path="+filepath.Join(*localdcsPath, "cert.pem"),
		"-tls_key_path="+filepath.Join(*localdcsPath, "key.pem"),
		"-listen_address="+*listenIndexBackend,
		"-tls_require_client_auth=false"); err != nil {
		log.Fatal(err.Error())
	}

	// TODO: check for healthiness

	log.Printf("dcs-index-backend running at https://%s\n", *listenIndexBackend)

	if err := launchInBackground(
		"dcs-source-backend",
		"-varz_avail_fs=",
		"-unpacked_path="+*unpackedPath,
		"-ranking_data_path="+rankingPath,
		"-tls_cert_path="+filepath.Join(*localdcsPath, "cert.pem"),
		"-tls_key_path="+filepath.Join(*localdcsPath, "key.pem"),
		"-listen_address="+*listenSourceBackend,
		"-tls_require_client_auth=false"); err != nil {
		log.Fatal(err.Error())
	}

	// TODO: check for healthiness

	log.Printf("dcs-source-backend running at https://%s\n", *listenSourceBackend)

	if err := launchInBackground(
		"dcs-web",
		"-varz_avail_fs=",
		"-template_pattern=cmd/dcs-web/templates/*",
		"-static_path=static/",
		"-source_backends="+*listenSourceBackend,
		"-tls_cert_path="+filepath.Join(*localdcsPath, "cert.pem"),
		"-tls_key_path="+filepath.Join(*localdcsPath, "key.pem"),
		"-listen_address="+*listenWeb); err != nil {
		log.Fatal(err.Error())
	}

	log.Printf("dcs-web running at http://%s\n", *listenWeb)
}
