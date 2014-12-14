package proto

// AUTO GENERATED - DO NOT EDIT

import (
	C "github.com/glycerine/go-capnproto"
	"unsafe"
)

type ProgressUpdate C.Struct

func NewProgressUpdate(s *C.Segment) ProgressUpdate      { return ProgressUpdate(s.NewStruct(16, 0)) }
func NewRootProgressUpdate(s *C.Segment) ProgressUpdate  { return ProgressUpdate(s.NewRootStruct(16, 0)) }
func AutoNewProgressUpdate(s *C.Segment) ProgressUpdate  { return ProgressUpdate(s.NewStructAR(16, 0)) }
func ReadRootProgressUpdate(s *C.Segment) ProgressUpdate { return ProgressUpdate(s.Root(0).ToStruct()) }
func (s ProgressUpdate) Filesprocessed() uint64          { return C.Struct(s).Get64(0) }
func (s ProgressUpdate) SetFilesprocessed(v uint64)      { C.Struct(s).Set64(0, v) }
func (s ProgressUpdate) Filestotal() uint64              { return C.Struct(s).Get64(8) }
func (s ProgressUpdate) SetFilestotal(v uint64)          { C.Struct(s).Set64(8, v) }

// capn.JSON_enabled == false so we stub MarshallJSON().
func (s ProgressUpdate) MarshalJSON() (bs []byte, err error) { return }

type ProgressUpdate_List C.PointerList

func NewProgressUpdateList(s *C.Segment, sz int) ProgressUpdate_List {
	return ProgressUpdate_List(s.NewCompositeList(16, 0, sz))
}
func (s ProgressUpdate_List) Len() int { return C.PointerList(s).Len() }
func (s ProgressUpdate_List) At(i int) ProgressUpdate {
	return ProgressUpdate(C.PointerList(s).At(i).ToStruct())
}
func (s ProgressUpdate_List) ToArray() []ProgressUpdate {
	return *(*[]ProgressUpdate)(unsafe.Pointer(C.PointerList(s).ToArray()))
}
func (s ProgressUpdate_List) Set(i int, item ProgressUpdate) { C.PointerList(s).Set(i, C.Object(item)) }
