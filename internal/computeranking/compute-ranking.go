package computeranking

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"pault.ag/go/debian/control"
)

func loadMirroredControlFile(mirrorURL, name string) ([]control.Paragraph, error) {
	url := fmt.Sprintf("%s/dists/sid/main/%s", mirrorURL, name)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		log.Fatalf("URL %q resulted in %v\n", url, resp.Status)
	}
	defer resp.Body.Close()

	reader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, err
	}
	pr, err := control.NewParagraphReader(reader, nil)
	if err != nil {
		return nil, err
	}
	contents, err := pr.All()
	if err != nil {
		return nil, err
	}

	return contents, nil
}

func Main(mirrorURL, outputPath string, verbose bool) error {
	flag.Parse()

	sourcePackages, err := loadMirroredControlFile(mirrorURL, "source/Sources.gz")
	if err != nil {
		return err
	}
	binaryPackages, err := loadMirroredControlFile(mirrorURL, "binary-amd64/Packages.gz")
	if err != nil {
		return err
	}

	popconInstSrc, err := popconInstallations(binaryPackages, verbose)
	if err != nil {
		return err
	}
	// Normalize the installation count.
	var totalInstallations float32
	for _, insts := range popconInstSrc {
		totalInstallations += insts
	}
	for srcpkg, insts := range popconInstSrc {
		// We multiply 1000 here because all values are < 0.0009.
		popconInstSrc[srcpkg] = (insts / totalInstallations) * 1000
	}

	reverseDeps := make(map[string]uint)
	for _, pkg := range binaryPackages {
		// We need to filter duplicates, because consider this:
		// agda-bin Recommends: libghc-agda-dev (>= 2.3.2), libghc-agda-dev (<< 2.3.2)
		dependsOn := make(map[string]bool)
		// NB: This differs from what apt-cache rdepends spit out. apt-cache
		// also considers the Replaces field.
		allDeps := pkg.Values["Depends"] + "," + pkg.Values["Suggests"] + "," + pkg.Values["Recommends"] + "," + pkg.Values["Enhances"]
		for _, dep := range strings.FieldsFunc(allDeps, func(r rune) bool {
			return r == ',' || r == '|'
		}) {
			trimmed := strings.TrimSpace(dep)
			spaceIdx := strings.Index(trimmed, " ")
			if spaceIdx == -1 {
				spaceIdx = len(trimmed)
			}
			dependsOn[trimmed[:spaceIdx]] = true
		}
		for name, _ := range dependsOn {
			reverseDeps[name] += 1
		}
	}

	type storedRanking struct {
		Inst float32
		Rdep float32
	}
	rankings := make(map[string]storedRanking)

	for _, pkg := range sourcePackages {
		rdepcount := float32(0)
		for packageName := range strings.SplitSeq(pkg.Values["Binary"], ",") {
			packageName = strings.TrimSpace(packageName)
			if packageName == "" {
				continue
			}
			rdepcount += float32(reverseDeps[packageName])
		}
		srcpkg := pkg.Values["Package"]
		packageRank := popconInstSrc[srcpkg]
		rdepcount = 1.0 - (1.0 / float32(rdepcount+1))
		if verbose {
			fmt.Printf("%f %f %s\n", packageRank, rdepcount, srcpkg)
		}
		rankings[srcpkg] = storedRanking{packageRank, rdepcount}
	}

	f, err := os.CreateTemp(filepath.Dir(outputPath), "dcs-compute-ranking")
	if err != nil {
		return err
	}

	if err := json.NewEncoder(f).Encode(rankings); err != nil {
		return err
	}

	if err := f.Close(); err != nil {
		return err
	}

	if err := os.Rename(f.Name(), outputPath); err != nil {
		return err
	}

	return nil
}
