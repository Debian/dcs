// Empty file containing instructions for “go generate” on how to rebuild the
// capnproto generated code.
package proto

//go:generate protoc indexbackend.proto sourcebackend.proto --go_out=plugins=grpc:.
