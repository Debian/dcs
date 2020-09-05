package main

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/Debian/dcs/internal/api"
	"github.com/Debian/dcs/internal/apikeys"
	"github.com/Debian/dcs/internal/proto/sourcebackendpb"
	"github.com/edsrzf/mmap-go"
	"github.com/golang/protobuf/proto"
	opentracing "github.com/opentracing/opentracing-go"
)

type resultWriter struct {
	perBackend []mmap.MMap
	w          io.Writer
	enc        *json.Encoder
	msg        sourcebackendpb.SearchReply
}

func (rw *resultWriter) fromPointers(pointers []resultPointer) error {
	for idx, ptr := range pointers {
		mapping := rw.perBackend[ptr.backendidx]
		if err := proto.Unmarshal(mapping[ptr.offset:ptr.offset+int64(ptr.length)], &rw.msg); err != nil {
			return err
		}
		if rw.msg.Type != sourcebackendpb.SearchReply_MATCH {
			continue
		}

		if idx > 0 {
			rw.w.Write([]byte{','})
		}
		m := rw.msg.Match
		contextBefore := make([]string, 0, 2)
		if m.Ctxp2 != "" {
			contextBefore = append(contextBefore, m.Ctxp2)
		}
		if m.Ctxp1 != "" {
			contextBefore = append(contextBefore, m.Ctxp1)
		}
		contextAfter := make([]string, 0, 2)
		if m.Ctxn1 != "" {
			contextAfter = append(contextAfter, m.Ctxn1)
		}
		if m.Ctxn2 != "" {
			contextAfter = append(contextAfter, m.Ctxn2)
		}
		if err := rw.enc.Encode(&api.SearchResult{
			Package:       m.Package,
			Path:          m.Path,
			Line:          m.Line,
			Context:       m.Context,
			ContextBefore: contextBefore,
			ContextAfter:  contextAfter,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (rw *resultWriter) Close() error {
	rw.w.Write([]byte{']'})
	for _, mapping := range rw.perBackend {
		if err := mapping.Unmap(); err != nil {
			return err
		}
	}
	return nil
}

func perBackendFromState(state queryState) ([]mmap.MMap, error) {
	state.tempFilesMu.Lock()
	perBackend := make([]mmap.MMap, len(state.perBackend))
	for idx, state := range state.perBackend {
		mapping, err := mmap.Map(state.tempFile, 0, 0)
		if err != nil {
			return nil, err
		}
		perBackend[idx] = mapping
	}
	state.tempFilesMu.Unlock()
	return perBackend, nil
}

func resultWriterFor(w io.Writer, state queryState) (*resultWriter, error) {
	perBackend, err := perBackendFromState(state)
	if err != nil {
		return nil, err
	}

	w.Write([]byte{'['})

	return &resultWriter{
		perBackend: perBackend,
		w:          w,
		enc:        json.NewEncoder(w),
	}, nil
}

func writeSearchResults(w io.Writer, state queryState) error {
	rw, err := resultWriterFor(w, state)
	if err != nil {
		return err
	}
	defer rw.Close()
	if err := rw.fromPointers(state.resultPointers); err != nil {
		return err
	}
	return nil
}

func writePerPackageSearchResults(w io.Writer, state queryState) error {
	rw, err := resultWriterFor(w, state)
	if err != nil {
		return err
	}
	defer rw.Close()
	for idx, pkg := range state.allPackagesSorted {
		if idx == 0 {
			fmt.Fprintf(w, `{"package": "%s", "results":[`, pkg)
		} else {
			fmt.Fprintf(w, `,{"package": "%s", "results":[`, pkg)
		}

		if err := rw.fromPointers(state.resultPointersByPkg[pkg]); err != nil {
			return err
		}
		w.Write([]byte{']', '}'})
	}
	return nil
}

func httpErrorWrapper(h func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			log.Println(err)
		}
	})
}

type apiserver struct {
	decoder *apikeys.Decoder
}

func (a *apiserver) common(w http.ResponseWriter, r *http.Request, writeResults func(io.Writer, queryState) error) error {
	ctx := r.Context()

	// Cross-Origin API requests are allowed. Like all other API requests,
	// they too must set a valid x-dcs-apikey header.
	w.Header().Set("Allow", "OPTIONS, GET")
	w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
	w.Header().Set("Access-Control-Allow-Methods", "OPTIONS, GET")
	w.Header().Set("Access-Control-Allow-Headers", "x-dcs-apikey, Content-Type")
	w.Header().Set("Access-Control-Max-Age", "86400")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}

	key, err := a.decoder.Decode(r.Header.Get("x-dcs-apikey"))
	if err != nil {
		http.Error(w, "invalid x-dcs-apikey header", http.StatusForbidden)
		return nil
	}

	src := key.Subject + "@" + r.RemoteAddr

	query := r.FormValue("query")
	if query == "" {
		http.Error(w, "no query parameter specified", http.StatusBadRequest)
		return nil
	}

	literal := r.FormValue("literal")
	if literal == "" {
		literal = "0"
	}

	if span := opentracing.SpanFromContext(ctx); span != nil {
		span.SetOperationName("API: " + query)
	}

	// We encode a URL that contains _only_ the q parameter.
	q := url.Values{"q": []string{query}}.Encode() + "&literal=" + literal

	// Uniquely (well, good enough) identify this query for a couple of minutes
	// (as long as we want to cache results). We could try to normalize the
	// query before hashing it, but that seems hardly worth the complexity.
	h := fnv.New64()
	io.WriteString(h, q)
	queryid := fmt.Sprintf("%x", h.Sum64())

	log.Printf("api(%q, %q, %q)\n", queryid, src, q)

	if err := validateQuery("?" + q); err != nil {
		return fmt.Errorf("Invalid query: %v", err)
	}

	if _, err := maybeStartQuery(ctx, queryid, src, q); err != nil {
		return fmt.Errorf("Could not start query: %v", err)
	}

	// TODO: more efficient way than polling to get notified when the query
	// is done
	for !queryCompleted(queryid) {
		time.Sleep(10 * time.Millisecond)
	}

	log.Printf("[%s] serving API results\n", queryid)

	stateMu.RLock()
	state := state[queryid]
	stateMu.RUnlock()

	startJsonResponse(w)

	if err := writeResults(w, state); err != nil {
		return err
	}

	return nil
}

func (a *apiserver) search(w http.ResponseWriter, r *http.Request) error {
	return a.common(w, r, writeSearchResults)
}

func (a *apiserver) searchperpackage(w http.ResponseWriter, r *http.Request) error {
	return a.common(w, r, writePerPackageSearchResults)
}

func serveAPIOnMux(mux *http.ServeMux, apiOpts apikeys.Options) error {
	a := &apiserver{
		decoder: &apikeys.Decoder{
			SecureCookie: apiOpts.SecureCookie(),
		},
	}
	mux.Handle("/v1/search", httpErrorWrapper(a.search))
	mux.Handle("/v1/searchperpackage", httpErrorWrapper(a.searchperpackage))
	return nil
}
