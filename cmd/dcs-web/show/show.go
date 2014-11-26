// vim:ts=4:sw=4:noexpandtab
package show

import (
	"fmt"
	"github.com/Debian/dcs/cmd/dcs-web/common"
	"github.com/Debian/dcs/cmd/dcs-web/health"
	"github.com/Debian/dcs/shardmapping"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
)

func Show(w http.ResponseWriter, r *http.Request) {
	query := r.URL
	filename := query.Query().Get("file")
	line64, err := strconv.ParseInt(query.Query().Get("line"), 10, 0)
	if err != nil {
		log.Printf("%v\n", err)
		return
	}
	line := int(line64)
	log.Printf("Showing file %s, line %d\n", filename, line)

	if *common.UseSourcesDebianNet && health.IsHealthy("sources.debian.net") {
		destination := fmt.Sprintf("http://sources.debian.net/src/%s?hl=%d#L%d",
			strings.Replace(filename, "_", "/", 1), line, line)
		log.Printf("SDN is healthy. Redirecting to %s\n", destination)
		http.Redirect(w, r, destination, 302)
		return
	}

	idx := strings.Index(filename, "/")
	if idx == -1 {
		http.Error(w, "Filename does not contain a package", http.StatusInternalServerError)
		return
	}
	pkg := filename[:idx]
	shards := strings.Split(*common.SourceBackends, ",")
	shard := shards[shardmapping.TaskIdxForPackage(pkg, len(shards))]

	queryCopy := query
	queryCopy.Scheme = "http"
	queryCopy.Host = shard
	queryCopy.Path = "/file"

	log.Printf("Asking source backend: %s\n", queryCopy.String())
	resp, err := http.Get(queryCopy.String())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	contents, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("%v\n", err)
		return
	}

	if resp.StatusCode != 200 {
		// relay the source backend error
		http.Error(w, string(contents), resp.StatusCode)
		return
	}

	// NB: contents is untrusted as it can contain the contents of any file
	// within any Debian package. Converting it to string is not a problem,
	// though, see http://golang.org/ref/spec#Conversions, "Conversions to and
	// from a string type": "Converting a slice of bytes to a string type
	// yields a string whose successive bytes are the elements of the slice.".
	// We don’t iterate over this string, we just pass it directly to the
	// user’s browser, which can then deal with the bytes :-).
	lines := strings.Split(string(contents), "\n")
	highestLineNr := fmt.Sprintf("%d", len(lines))

	// Since Go templates don’t offer any way to use {{$idx+1}}, we need to
	// pre-calculate line numbers starting from 1 here.
	lineNumbers := make([]int, len(lines))
	for idx, _ := range lines {
		lineNumbers[idx] = idx + 1
	}

	err = common.Templates.ExecuteTemplate(w, "show.html", map[string]interface{}{
		"line":     line,
		"lines":    lines,
		"numbers":  lineNumbers,
		"lnrwidth": len(highestLineNr),
		"filename": filename,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
