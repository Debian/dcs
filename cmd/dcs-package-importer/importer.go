// Accepts Debian packages via HTTP, unpacks, strips and indexes them.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Debian/dcs/index"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	listenAddress = flag.String("listen_address",
		":21010",
		"listen address ([host]:port)")

	unpackedPath = flag.String("unpacked_path",
		"/dcs-ssd/unpacked/",
		"Path to the unpacked sources")

	cpuProfile = flag.String("cpuprofile",
		"",
		"write cpu profile to this file")

	tmpdir string

	indexQueue chan string
	mergeQueue chan bool
)

// Accepts arbitrary files for a given package and starts unpacking once a .dsc
// file is uploaded. E.g.:
//
// curl -X PUT --data-binary @i3-wm_4.7.2-1.debian.tar.xz \
//     http://localhost:21010/import/i3-wm_4.7.2-1/i3-wm_4.7.2-1.debian.tar.xz
// curl -X PUT --data-binary @i3-wm_4.7.2.orig.tar.bz2 \
//     http://localhost:21010/import/i3-wm_4.7.2-1/i3-wm_4.7.2.orig.tar.bz2
// curl -X PUT --data-binary @i3-wm_4.7.2-1.dsc \
//     http://localhost:21010/import/i3-wm_4.7.2-1/i3-wm_4.7.2-1.dsc
//
// All the files are stored in the same directory and after the .dsc is stored,
// the package is unpacked with dpkg-source, then indexed.
func importPackage(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	path := r.URL.Path[len("/import/"):]
	pkg := filepath.Dir(path)
	filename := filepath.Base(path)

	err := os.Mkdir(filepath.Join(tmpdir, pkg), 0755)
	if err != nil && !os.IsExist(err) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	file, err := os.Create(filepath.Join(tmpdir, path))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer file.Close()
	written, err := io.Copy(file, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Wrote %d bytes into %s\n", written, path)

	fmt.Fprintf(w, "thank you for sending file %s for package %s!\n", filename, pkg)
	if strings.HasSuffix(filename, ".dsc") {
		indexQueue <- path
	}
}

// Tries to start a merge and errors in case one is already in progress.
func mergeOrError(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	select {
	case mergeQueue <- true:
		fmt.Fprintf(w, "Merge started.")
	default:
		http.Error(w, "Merge already in progress, please try again later.", http.StatusInternalServerError)
	}
}

func packageNames() []string {
	var names []string

	file, err := os.Open(*unpackedPath)
	// If the directory does not yet exist, we just return an empty list of
	// packages.
	if err != nil && !os.IsNotExist(err) {
		log.Fatal(err)
	}
	if err == nil {
		defer file.Close()
		names, err = file.Readdirnames(-1)
		if err != nil {
			log.Fatal(err)
		}
	}

	return names
}

func listPackages(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	names := packageNames()

	type ListPackageReply struct {
		Packages []string
	}

	var reply ListPackageReply
	reply.Packages = make([]string, 0, len(names))
	for _, name := range names {
		if strings.HasSuffix(name, ".idx") {
			reply.Packages = append(reply.Packages, name[:len(name)-len(".idx")])
		}
	}

	jsonReply, err := json.Marshal(&reply)
	if err != nil {
		http.Error(w, fmt.Sprintf("Serialization error: %v", err), http.StatusInternalServerError)
		return
	}

	if _, err := w.Write(jsonReply); err != nil {
		log.Printf("Could not send listPackages reply: %v\n", err)
	}
}

func garbageCollect(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	pkg := r.FormValue("package")
	if pkg == "" {
		http.Error(w, "No ?package= provided", http.StatusInternalServerError)
		return
	}

	names := packageNames()
	found := false
	for _, name := range names {
		// Note that the logic is inverted in comparison to earlier in the
		// code: for listPackages, we want to only return packages that have
		// been unpacked and indexed (so we strip .idx), but for garbage
		// collection, we also want to garbage collect packages that were not
		// indexed for some reason, so we ignore .idx.
		if name == pkg && !strings.HasSuffix(name, ".idx") {
			found = true
			break
		}
	}

	if !found {
		http.Error(w, "No such package", http.StatusInternalServerError)
		return
	}

	if err := os.RemoveAll(filepath.Join(*unpackedPath, pkg)); err != nil {
		http.Error(w, fmt.Sprintf("Could not garbage collect package %q: %v", pkg, err), http.StatusInternalServerError)
		return
	}

	if err := os.Remove(filepath.Join(*unpackedPath, pkg+".idx")); err != nil {
		http.Error(w, fmt.Sprintf("Could not garbage collect package index for %q: %v", pkg, err), http.StatusInternalServerError)
		return
	}
}

// Merges all packages in *unpackedPath into a big index shard.
func mergeToShard() {
	names := packageNames()
	indexFiles := make([]string, 0, len(names))
	for _, name := range names {
		if strings.HasSuffix(name, ".idx") && name != "full.idx" {
			indexFiles = append(indexFiles, filepath.Join(*unpackedPath, name))
		}
	}

	log.Printf("Got %d index files\n", len(indexFiles))
	if len(indexFiles) == 1 {
		return
	}
	tmpIndexPath, err := ioutil.TempFile(*unpackedPath, "newshard")
	if err != nil {
		log.Fatal(err)
	}

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	t0 := time.Now()
	index.ConcatN(tmpIndexPath.Name(), indexFiles...)
	t1 := time.Now()
	log.Printf("merged in %v\n", t1.Sub(t0))
	//for i := 1; i < len(indexFiles); i++ {
	//	log.Printf("merging %s with %s\n", indexFiles[i-1], indexFiles[i])
	//	t0 := time.Now()
	//	index.Concat(tmpIndexPath.Name(), indexFiles[i-1], indexFiles[i])
	//	t1 := time.Now()
	//	log.Printf("merged in %v\n", t1.Sub(t0))
	//}
	log.Printf("merged into shard %s\n", tmpIndexPath.Name())

	// If full.idx does not exist (i.e. on initial deployment), just move the
	// new index to full.idx, the dcs-index-backend will not be running anyway.
	fullIdxPath := filepath.Join(*unpackedPath, "full.idx")
	if _, err := os.Stat(fullIdxPath); os.IsNotExist(err) {
		if err := os.Rename(tmpIndexPath.Name(), fullIdxPath); err != nil {
			log.Fatal(err)
		}
		return
	}

	// Replace the current index with the newly created index.
	resp, err := http.Get(fmt.Sprintf("http://localhost:28081/replace?shard=%s", filepath.Base(tmpIndexPath.Name())))
	if err != nil {
		log.Fatal(err)
	}

	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		log.Fatalf("dcs-index-backend /replace response: %+v (body: %s)\n", resp, body)
	}
}

func indexPackage(pkg string) {
	unpacked := filepath.Join(tmpdir, pkg, pkg)
	if err := os.MkdirAll(*unpackedPath, os.FileMode(0755)); err != nil {
		log.Fatalf("Could not create directory: %v\n", err)
	}
	index := index.Create(filepath.Join(*unpackedPath, pkg+".idx"))
	// +1 because of the / that should not be included in the index.
	stripLen := len(filepath.Join(tmpdir, pkg)) + 1

	filepath.Walk(unpacked,
		func(path string, info os.FileInfo, err error) error {
			if dir, filename := filepath.Split(path); filename != "" {
				skip := ignored(info, dir, filename)
				if skip && info.IsDir() {
					if err := os.RemoveAll(path); err != nil {
						log.Fatalf("Could not remove directory %q: %v\n", path, err)
					}
					return filepath.SkipDir
				}
				if skip && !info.IsDir() {
					if err := os.Remove(path); err != nil {
						log.Fatalf("Could not remove file %q: %v\n", path, err)
					}
					return nil
				}
			}

			if info == nil || !info.Mode().IsRegular() {
				return nil
			}

			// Some filenames (e.g.
			// "xblast-tnt-levels_20050106-2/reconstruct\xeeon2.xal") contain
			// invalid UTF-8 and will break when sending them via JSON later
			// on. Filter those out early to avoid breakage.
			if !utf8.ValidString(path) {
				log.Printf("Skipping due to invalid UTF-8: %s\n", path)
				return nil
			}

			if err := index.AddFile(path, path[stripLen:]); err != nil {
				if err := os.Remove(path); err != nil {
					log.Fatalf("Could not remove file %q: %v\n", path, err)
				}
			} else {
				// Copy this file out of /tmp to our unpacked directory.
				outputPath := filepath.Join(*unpackedPath, path[stripLen:])
				if err := os.MkdirAll(filepath.Dir(outputPath), os.FileMode(0755)); err != nil {
					log.Fatalf("Could not create directory: %v\n", err)
				}
				output, err := os.Create(outputPath)
				if err != nil {
					log.Fatalf("Could not create output file %q: %v\n", outputPath, err)
				}
				defer output.Close()
				input, err := os.Open(path)
				if err != nil {
					log.Fatalf("Could not open input file %q: %v\n", path, err)
				}
				defer input.Close()
				if _, err := io.Copy(output, input); err != nil {
					log.Fatalf("Could not copy %q to %q: %v\n", path, outputPath, err)
				}
			}
			return nil
		})

	index.Flush()
}

// This goroutine reads package names from the indexQueue channel, unpacks the
// package, deletes all unnecessary files and indexes it.
// By default, the number of simultaneous goroutines running this function is
// equal to your number of CPUs.
func unpackAndIndex() {
	for {
		dscPath := <-indexQueue
		pkg := filepath.Dir(dscPath)
		log.Printf("Unpacking %s\n", pkg)
		unpacked := filepath.Join(tmpdir, pkg, pkg)

		cmd := exec.Command("dpkg-source", "--no-copy", "--no-check", "-x",
			filepath.Join(tmpdir, dscPath), unpacked)
		// Just display dpkg-source’s stderr in our process’s stderr.
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Printf("Skipping package %s: %v\n", pkg, err)
			continue
		}

		indexPackage(pkg)
		os.RemoveAll(filepath.Join(tmpdir, pkg))
	}
}

func main() {
	flag.Parse()

	// Allow as many concurrent unpackAndIndex goroutines as we have cores.
	runtime.GOMAXPROCS(runtime.NumCPU())

	setupFilters()

	var err error
	tmpdir, err = ioutil.TempDir("", "dcs-importer")
	if err != nil {
		log.Fatal(err)
	}

	indexQueue = make(chan string)
	mergeQueue = make(chan bool)

	for i := 0; i < runtime.NumCPU(); i++ {
		go unpackAndIndex()
	}

	go func() {
		for _ = range mergeQueue {
			mergeToShard()
		}
	}()

	http.HandleFunc("/import/", importPackage)
	http.HandleFunc("/merge", mergeOrError)
	http.HandleFunc("/listpkgs", listPackages)
	http.HandleFunc("/garbagecollect", garbageCollect)

	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
