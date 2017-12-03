// vim:ts=4:sw=4:noexpandtab
package show

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Debian/dcs/cmd/dcs-web/common"
	"github.com/Debian/dcs/cmd/dcs-web/health"
	"github.com/Debian/dcs/proto"
	"github.com/Debian/dcs/shardmapping"
	"golang.org/x/net/context"
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

	if *common.UseSourcesDebianNet && health.IsHealthy("sources.debian.org") {
		destination := fmt.Sprintf("https://sources.debian.org/src/%s?hl=%d#L%d",
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
	shard := common.SourceBackendStubs[shardmapping.TaskIdxForPackage(pkg, len(common.SourceBackendStubs))]
	resp, err := shard.File(context.Background(), &proto.FileRequest{
		Path: filename,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// NB: contents is untrusted as it can contain the contents of any file
	// within any Debian package. Converting it to string is not a problem,
	// though, see http://golang.org/ref/spec#Conversions, "Conversions to and
	// from a string type": "Converting a slice of bytes to a string type
	// yields a string whose successive bytes are the elements of the slice.".
	// We don’t iterate over this string, we just pass it directly to the
	// user’s browser, which can then deal with the bytes :-).
	lines := strings.Split(string(resp.Contents), "\n")
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
