package localdcs

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/Debian/dcs/grpcutil"
	"github.com/Debian/dcs/internal/proto/packageimporterpb"
)

var (
	stop = flag.Bool("stop",
		false,
		"Whether to stop the currently running localdcs instead of starting a new one")
	shardPath = flag.String("shard_path",
		"/tmp/dcs-hacking",
		"Path to the unpacked sources")
	localdcsPath = flag.String("localdcs_path",
		"~/.config/dcs-localdcs",
		"Directory in which to keep state for dcs-localdcs (TLS certificates, PID files, etc.)")
	listenPackageImporter = flag.String("listen_package_importer",
		"localhost:0",
		"listen address ([host]:port) for dcs-package-importer")
	listenIndexBackend = flag.String("listen_index_backend",
		"localhost:0",
		"listen address ([host]:port) for dcs-index-backend")
	listenSourceBackend = flag.String("listen_source_backend",
		"localhost:0",
		"listen address ([host]:port) for dcs-source-backend")
	listenWeb = flag.String("listen_web",
		"localhost:0",
		"listen address ([host]:port) for dcs-web (gRPC/TLS)")
)

func installBinaries() error {
	cmd := exec.Command("go", "install", "-ldflags", "-X github.com/Debian/dcs/cmd/dcs-web/common.Version=git", "github.com/Debian/dcs/cmd/...")
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

func launchInBackground(binary string, args ...string) (addr string, _ error) {
	// TODO: redirect stderr into a file
	cmd := exec.Command(binary, args...)
	r, w, err := os.Pipe()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{w}
	// Put binaries into a separate process group, so that they survive when
	// dcs-localdcs terminates.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	cmd.Args = append(cmd.Args, "-addrfd=3") // Go dup2()s ExtraFiles to 3 and onwards

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("Could not start %q: %v", binary, err)
	}

	// Close the write end of the pipe in the parent process.
	if err := w.Close(); err != nil {
		return "", err
	}

	log.Printf("reading from pair[0]")
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return "", err
	}
	addr = string(b)

	if err := recordResource("pid", strconv.Itoa(cmd.Process.Pid)); err != nil {
		return "", fmt.Errorf("Could not record pid of %q: %v", binary, err)
	}
	return addr, nil
}

func feed(packageImporter packageimporterpb.PackageImporterClient, pkg, file string) error {
	b, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}
	stream, err := packageImporter.Import(context.Background())
	if err != nil {
		return err
	}
	if err := stream.Send(&packageimporterpb.ImportRequest{
		SourcePackage: pkg,
		Filename:      filepath.Base(file),
		Content:       b,
	}); err != nil {
		return err
	}
	_, err = stream.CloseAndRecv()
	return err
}

func importTestdata(packageImporterAddr string) error {
	conn, err := grpcutil.DialTLS(
		packageImporterAddr,
		filepath.Join(*localdcsPath, "cert.pem"),
		filepath.Join(*localdcsPath, "key.pem"),
		grpc.WithBlock())
	if err != nil {
		return fmt.Errorf("grpcutil.DialTLS(%s): %v", packageImporterAddr, err)
	}
	packageImporter := packageimporterpb.NewPackageImporterClient(conn)
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
			if err := feed(packageImporter, pkg, file); err != nil {
				return err
			}
		}
	}

	_, err = packageImporter.Merge(context.Background(), &packageimporterpb.MergeRequest{})
	return err
}

func Start() (addr string, _ error) {
	if len(*localdcsPath) >= 2 && (*localdcsPath)[:2] == "~/" {
		usr, err := user.Current()
		if err != nil {
			return "", fmt.Errorf("Cannot expand -localdcs_path: %v", err)
		}
		*localdcsPath = strings.Replace(*localdcsPath, "~/", usr.HomeDir+"/", 1)
	}

	if err := os.MkdirAll(*localdcsPath, 0700); err != nil {
		return "", fmt.Errorf("Could not create directory %q for dcs-localdcs state: %v", *localdcsPath, err)
	}

	if *stop {
		if err := kill(); err != nil {
			return "", fmt.Errorf("Could not stop localdcs: %v", err)
		}
		return "", nil
	}

	if _, err := os.Stat(filepath.Join(*localdcsPath, "pids")); !os.IsNotExist(err) {
		return "", fmt.Errorf("There already is a localdcs instance running. Either use -stop or specify a different -localdcs_path")
	}

	for _, dir := range []string{
		*shardPath,
		filepath.Join(*shardPath, "src"),
		filepath.Join(*shardPath, "idx"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("Could not create directory %q for unpacked files/index: %v", dir, err)
		}
	}

	if err := installBinaries(); err != nil {
		return "", fmt.Errorf("Compiling and installing binaries failed: %v", err)
	}

	if err := verifyBinariesAreExecutable(); err != nil {
		return "", fmt.Errorf("Could not find all required binaries: %v", err)
	}

	if err := compileStaticAssets(); err != nil {
		return "", fmt.Errorf("Compiling static assets failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(*localdcsPath, "key.pem")); os.IsNotExist(err) {
		log.Printf("Generating TLS certificate\n")
		if err := generatecert(*localdcsPath); err != nil {
			return "", fmt.Errorf("Could not generate TLS certificate: %v", err)
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
			return "", fmt.Errorf("Could not compute ranking data: %v", err)
		}
	} else {
		log.Printf("Recent-enough rankings file %q found, not re-generating (delete to force)\n", rankingPath)
	}

	sourceBackend, err := launchInBackground(
		"dcs-source-backend",
		"-index_path="+filepath.Join(*shardPath, "full"),
		"-varz_avail_fs=",
		"-unpacked_path="+filepath.Join(*shardPath, "src"),
		"-ranking_data_path="+rankingPath,
		"-tls_cert_path="+filepath.Join(*localdcsPath, "cert.pem"),
		"-tls_key_path="+filepath.Join(*localdcsPath, "key.pem"),
		"-listen_address="+*listenSourceBackend,
		"-tls_require_client_auth=false")
	if err != nil {
		return "", err
	}

	// Start package importer and import testdata/
	packageImporter, err := launchInBackground(
		"dcs-package-importer",
		"-source_backend="+sourceBackend,
		"-debug_skip",
		"-varz_avail_fs=",
		"-tls_cert_path="+filepath.Join(*localdcsPath, "cert.pem"),
		"-tls_key_path="+filepath.Join(*localdcsPath, "key.pem"),
		"-shard_path="+*shardPath,
		"-listen_address="+*listenPackageImporter,
		"-tls_require_client_auth=false")
	if err != nil {
		return "", err
	}
	if err := importTestdata(packageImporter); err != nil {
		return "", fmt.Errorf("Could not import testdata/: %v", err)
	}

	// TODO: check for healthiness

	dcsWeb, err := launchInBackground(
		"dcs-web",
		"-varz_avail_fs=",
		"-headroom_percentage=0",
		"-template_pattern=cmd/dcs-web/templates/*",
		"-static_path=static/",
		"-source_backends="+sourceBackend,
		"-tls_cert_path="+filepath.Join(*localdcsPath, "cert.pem"),
		"-tls_key_path="+filepath.Join(*localdcsPath, "key.pem"),
		"-listen_address="+*listenWeb,
		"-tls_require_client_auth=false")
	if err != nil {
		return "", err
	}

	log.Printf("dcs-web running at https://%s\n", dcsWeb)
	// TODO: introduce a test option, prevent this code from running, handle errors
	exec.Command("google-chrome", "https://"+dcsWeb).Run() // ignore errors (for tests)
	return dcsWeb, nil
}
