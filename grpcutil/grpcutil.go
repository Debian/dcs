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

	"github.com/Debian/dcs/internal/addrfd"
	"github.com/grpc-ecosystem/go-grpc-middleware/tracing/opentracing"
	"golang.org/x/net/http2"
	"golang.org/x/net/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	requireClientAuth = flag.Bool("tls_require_client_auth",
		true,
		"Require TLS Client Authentication")
)

func init() {
	// Disable grpc tracing until
	// https://github.com/grpc/grpc-go/issues/695 is fixed.
	grpc.EnableTracing = false
}

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

func DialTLS(addr, certFile, keyFile string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
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

	return grpc.Dial(addr,
		append([]grpc.DialOption{
			grpc.WithTransportCredentials(auth),
			grpc.WithStreamInterceptor(grpc_opentracing.StreamClientInterceptor()),
			grpc.WithUnaryInterceptor(grpc_opentracing.UnaryClientInterceptor()),
		}, opts...)...)
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

	s := grpc.NewServer(
		grpc.Creds(auth),
		grpc.StreamInterceptor(grpc_opentracing.StreamServerInterceptor()),
		grpc.UnaryInterceptor(grpc_opentracing.UnaryServerInterceptor()))

	register(s)

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
		trace.AuthRequest = func(req *http.Request) (bool, bool) {
			return true, true
		}
	}
	srv.TLSConfig.Certificates = make([]tls.Certificate, 1)
	srv.TLSConfig.Certificates[0], err = tls.LoadX509KeyPair(certFile, keyFile)
	addrfd.MustWrite(ln.Addr().String())
	return srv.Serve(tls.NewListener(ln, srv.TLSConfig))
}
