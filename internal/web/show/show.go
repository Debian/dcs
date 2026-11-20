// vim:ts=4:sw=4:noexpandtab
package show

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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

const showUsage = `usage: /show/package_version/path/to/file?line=139
  line=k     print line k of the file, with context=n lines
  line=k-l   print lines k to l of the file, with context=n lines
  context=n  print n (default: 10) context lines around the matching line(s)

examples:
  /show/i3-wm_4.5.1-1/libi3/font.c
  /show/i3-wm_4.5.1-1/libi3/font.c?line=139
  /show/i3-wm_4.5.1-1/libi3/font.c?line=130-140
  /show/i3-wm_4.5.1-1/libi3/font.c?line=139&context=0
`

type showRequest struct {
	lineFrom int
	lineTo   int
	context  int
}

func validateParameters(r *http.Request) (*showRequest, error) {
	req := &showRequest{
		context: 10, // show 10 lines before/after the requested by default
	}
	if lineStr := r.FormValue("line"); lineStr != "" {
		var err error
		if from, to, isRange := strings.Cut(lineStr, "-"); isRange {
			req.lineFrom, err = strconv.Atoi(from)
			if err != nil {
				return nil, errors.New("invalid line= parameter")
			}
			req.lineTo, err = strconv.Atoi(to)
			if err != nil {
				return nil, errors.New("invalid line= parameter")
			}
		} else {
			req.lineFrom, err = strconv.Atoi(from)
			if err != nil {
				return nil, errors.New("invalid line= parameter")
			}
			req.lineTo = req.lineFrom
		}
		if req.lineFrom < 1 {
			return nil, errors.New("invalid line= parameter (< 1)")
		}
		if req.lineTo < req.lineFrom {
			return nil, errors.New("invalid line= range (to < from)")
		}
	}
	if contextStr := r.FormValue("context"); contextStr != "" {
		n, err := strconv.Atoi(contextStr)
		if err != nil || n < 0 {
			return nil, errors.New("invalid context= parameter")
		}
		if req.lineFrom == 0 {
			return nil, errors.New("context= requires line=")
		}
		req.context = n
	}
	return req, nil
}

func ShowPlain(backends []common.Backend) http.Handler {
	return httperr.ErrorHandler(func(w http.ResponseWriter, r *http.Request) error {
		filename := r.PathValue("file")
		if filename == "" {
			// At /show/, return the usage message with HTTP 200.
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, err := w.Write([]byte(showUsage))
			return err
		}
		for param := range r.URL.Query() {
			if param == "line" || param == "context" {
				continue
			}
			return httperr.Error(http.StatusBadRequest, fmt.Errorf("unexpected query parameter %q. %s", param, showUsage))
		}
		req, err := validateParameters(r)
		if err != nil {
			return httperr.Error(http.StatusBadRequest, fmt.Errorf("%v. %s", err, showUsage))
		}
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
		if req.lineFrom == 0 {
			// Easy case: return the entire file.
			_, err = w.Write(resp)
			return err
		}
		// Return only a portion of the file, around the requested line (range).
		// We do one pass over the file and collect the matching+context lines,
		// then validate the request (when we know the number of lines)
		// and add the header.
		var buf bytes.Buffer
		n := 0
		resp = bytes.TrimSuffix(resp, []byte{'\n'})
		for line := range bytes.SplitSeq(resp, []byte{'\n'}) {
			n++
			if n < req.lineFrom-req.context || n-req.context > req.lineTo {
				continue
			}
			sep := "-" // context
			if n >= req.lineFrom && n <= req.lineTo {
				sep = ":" // match
			}
			fmt.Fprintf(&buf, "%d%s %s\n", n, sep, line)
		}
		if req.lineFrom > n {
			return httperr.Error(http.StatusBadRequest, fmt.Errorf("invalid line= parameter: %s has only %d lines. %s", filename, n, showUsage))
		}
		if req.lineTo != req.lineFrom {
			fmt.Fprintf(w, "# %s lines %d-%d (+context) of %d\n", filename, req.lineFrom, min(req.lineTo, n), n)
		} else {
			fmt.Fprintf(w, "# %s line %d (+context) of %d\n", filename, req.lineFrom, n)
		}
		_, err = io.Copy(w, &buf)
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
		if filename == "" {
			return httperr.Error(http.StatusBadRequest, fmt.Errorf("usage: /show/pkg_version/path/to/file"))
		}
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
