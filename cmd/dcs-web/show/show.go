// vim:ts=4:sw=4:noexpandtab
package show

import (
	"dcs/cmd/dcs-web/common"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
)

var workaroundPath = flag.String("workaround_path", "/dcs-ssd/unpacked/", "workaround until the TODO item is fulfilled")

func Show(w http.ResponseWriter, r *http.Request) {
	query := r.URL
	filename := query.Query().Get("file")
	line, err := strconv.ParseInt(query.Query().Get("line"), 10, 0)
	if err != nil {
		log.Printf("%v\n", err)
		return
	}
	log.Printf("Showing file %s, line %d\n", filename, line)

	// TODO: this needs to be a source-backend query instead
	file, err := os.Open(path.Join(*workaroundPath, filename))
	if err != nil {
		log.Printf("%v\n", err)
		return
	}
	defer file.Close()

	contents, err := ioutil.ReadAll(file)
	if err != nil {
		log.Printf("%v\n", err)
		return
	}

	// NB: contents is untrusted as it can containt the contents of any file
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
		"lines":    lines,
		"numbers":  lineNumbers,
		"lnrwidth": len(highestLineNr),
		"filename": filename,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
