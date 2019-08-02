// Package rpctest provides convenience functions for common RPC testing setups.
package rpctest

import (
	"context"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
)

// Loopback calls register to register services on a loopback gRPC server and
// returns a connection to it. No sockets are involved.
func Loopback(register func(s *grpc.Server)) (*grpc.ClientConn, func()) {
	ln := bufconn.Listen(4096 /* initial pipe buffer capacity */)
	s := grpc.NewServer()
	register(s)
	go func() {
		if err := s.Serve(ln); err != nil {
			log.Fatalf("s.Serve: %v", err)
		}
	}()
	conn, err := grpc.Dial(ln.Addr().String(),
		grpc.WithInsecure(),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return ln.Dial()
		}))
	if err != nil {
		panic(fmt.Sprintf("BUG: loopback gRPC: %v", err))
	}
	return conn, func() {
		conn.Close()
		s.Stop() // makes s.Serve return
	}
}
