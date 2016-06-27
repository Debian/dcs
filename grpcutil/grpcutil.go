// Encapsulates common RPC server setup.
package grpcutil

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strings"

	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	requireClientAuth = flag.Bool("tls_require_client_auth",
		true,
		"Require TLS Client Authentification")
)

// grpcHandlerFunc returns an http.Handler that delegates to grpcServer on incoming gRPC
// connections or otherHandler otherwise. Copied from cockroachdb.
func grpcHandlerFunc(grpcServer *grpc.Server, otherHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This is a partial recreation of gRPC's internal checks:
		// https://github.com/grpc/grpc-go/blob/7834b974e55fbf85a5b01afb5821391c71084efd/transport/handler_server.go#L61
		if r.ProtoMajor == 2 && strings.Contains(r.Header.Get("Content-Type"), "application/grpc") {
			grpcServer.ServeHTTP(w, r)
		} else {
			otherHandler.ServeHTTP(w, r)
		}
	})
}

func DialTLS(addr, certFile, keyFile string) (*grpc.ClientConn, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	contents, err := ioutil.ReadFile(certFile)
	if err != nil {
		return nil, err
	}
	if !roots.AppendCertsFromPEM(contents) {
		return nil, fmt.Errorf("Could not parse %q as PEM file (contents: %q)", certFile, contents)
	}
	auth := credentials.NewTLS(&tls.Config{
		RootCAs:      roots,
		Certificates: []tls.Certificate{cert}})

	return grpc.Dial(addr, grpc.WithTransportCredentials(auth))
}

func ListenAndServeTLS(addr, certFile, keyFile string, register func(s *grpc.Server)) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	auth, err := credentials.NewServerTLSFromFile(certFile, keyFile)
	if err != nil {
		return err
	}

	s := grpc.NewServer(grpc.Creds(auth))

	register(s)

	http.Handle("/", s)

	srv := http.Server{
		Addr:    addr,
		Handler: grpcHandlerFunc(s, http.DefaultServeMux),
	}
	if err := http2.ConfigureServer(&srv, nil); err != nil {
		return err
	}
	roots := x509.NewCertPool()
	contents, err := ioutil.ReadFile(certFile)
	if err != nil {
		return err
	}
	if !roots.AppendCertsFromPEM(contents) {
		return fmt.Errorf("Could not parse %q as PEM file (contents: %q)", certFile, contents)
	}

	if *requireClientAuth {
		srv.TLSConfig.ClientCAs = roots
		srv.TLSConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}
	srv.TLSConfig.Certificates = make([]tls.Certificate, 1)
	srv.TLSConfig.Certificates[0], err = tls.LoadX509KeyPair(certFile, keyFile)
	return srv.Serve(tls.NewListener(ln, srv.TLSConfig))
}
