// vim:ts=4:sw=4:noexpandtab
package show

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Debian/dcs/internal/httperr"
	"github.com/Debian/dcs/internal/shardmapping"
	"github.com/Debian/dcs/internal/web/common"
	"github.com/Debian/dcs/internal/web/health"
)

func ShowPlain(backends []common.Backend) http.Handler {
	return httperr.ErrorHandler(func(w http.ResponseWriter, r *http.Request) error {
		filename := r.PathValue("file")
		log.Printf("Showing file %s", filename)
		resp, err := readFile(backends, filename)
		if err != nil {
			return err
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Once uploaded to Debian, files cannot be changed.
		// Set the cache header to reflect file contents are immutable
		// and can be cached indefinitely.
		w.Header().Set("Cache-Control", "public, max-age=2592000, immutable")
		_, err = w.Write(resp)
		return err
	})
}

func readFile(backends []common.Backend, filename string) ([]byte, error) {
	pkg, _, ok := strings.Cut(filename, "/")
	if !ok {
		return nil, httperr.Error(http.StatusBadRequest, fmt.Errorf("Filename does not contain a package"))
	}
	shard := backends[shardmapping.TaskIdxForPackage(pkg, len(backends))]
	resp, err := shard.ReadFile(context.Background(), filename)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, httperr.Error(http.StatusNotFound, fmt.Errorf("file not found: %s", filename))
		}
		return nil, err
	}
	return resp, nil
}

func Show(backends []common.Backend, useSourcesDebianNet bool) http.Handler {
	return httperr.ErrorHandler(func(w http.ResponseWriter, r *http.Request) error {
		query := r.URL
		filename := query.Query().Get("file")
		line64, err := strconv.ParseInt(query.Query().Get("line"), 10, 0)
		if err != nil {
			return httperr.Error(http.StatusBadRequest, err)
		}
		line := int(line64)
		log.Printf("Showing file %s, line %d\n", filename, line)

		if useSourcesDebianNet && health.IsHealthy("sources.debian.org") {
			u, _ := url.Parse("https://sources.debian.org/")
			u.Path = "/src/" + strings.Replace(filename, "_", "/", 1)
			q := u.Query()
			q.Set("hl", strconv.Itoa(line))
			u.RawQuery = q.Encode()
			u.Fragment = "L" + strconv.Itoa(line)
			destination := u.String()
			log.Printf("SDN is healthy. Redirecting to %s\n", destination)
			http.Redirect(w, r, destination, 302)
			return nil
		}

		resp, err := readFile(backends, filename)
		if err != nil {
			return err
		}

		// NB: contents is untrusted as it can contain the contents of any file
		// within any Debian package. Converting it to string is not a problem,
		// though, see http://golang.org/ref/spec#Conversions, "Conversions to and
		// from a string type": "Converting a slice of bytes to a string type
		// yields a string whose successive bytes are the elements of the slice.".
		// We don’t iterate over this string, we just pass it directly to the
		// user’s browser, which can then deal with the bytes :-).
		lines := strings.Split(string(resp), "\n")
		highestLineNr := fmt.Sprintf("%d", len(lines))

		// Since Go templates don’t offer any way to use {{$idx+1}}, we need to
		// pre-calculate line numbers starting from 1 here.
		lineNumbers := make([]int, len(lines))
		for idx := range lines {
			lineNumbers[idx] = idx + 1
		}

		return common.Templates.ExecuteTemplate(w, "show.html", map[string]any{
			"line":     line,
			"lines":    lines,
			"numbers":  lineNumbers,
			"lnrwidth": len(highestLineNr),
			"filename": filename,
		})
	})
}
