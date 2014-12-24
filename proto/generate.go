// Empty file containing instructions for “go generate” on how to rebuild the
// capnproto generated code.
package proto

//go:generate capnp compile -I $GOPATH -ogo match.capnp
//go:generate capnp compile -I $GOPATH -ogo progressupdate.capnp
//go:generate capnp compile -I $GOPATH -ogo sourcebackend.capnp
