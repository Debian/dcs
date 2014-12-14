package proto

// AUTO GENERATED - DO NOT EDIT

import (
	C "github.com/glycerine/go-capnproto"
	"unsafe"
)

type Z C.Struct
type Z_Which uint16

const (
	Z_PROGRESSUPDATE Z_Which = 0
	Z_MATCH          Z_Which = 1
)

func NewZ(s *C.Segment) Z                  { return Z(s.NewStruct(8, 1)) }
func NewRootZ(s *C.Segment) Z              { return Z(s.NewRootStruct(8, 1)) }
func AutoNewZ(s *C.Segment) Z              { return Z(s.NewStructAR(8, 1)) }
func ReadRootZ(s *C.Segment) Z             { return Z(s.Root(0).ToStruct()) }
func (s Z) Which() Z_Which                 { return Z_Which(C.Struct(s).Get16(0)) }
func (s Z) Progressupdate() ProgressUpdate { return ProgressUpdate(C.Struct(s).GetObject(0).ToStruct()) }
func (s Z) SetProgressupdate(v ProgressUpdate) {
	C.Struct(s).Set16(0, 0)
	C.Struct(s).SetObject(0, C.Object(v))
}
func (s Z) Match() Match     { return Match(C.Struct(s).GetObject(0).ToStruct()) }
func (s Z) SetMatch(v Match) { C.Struct(s).Set16(0, 1); C.Struct(s).SetObject(0, C.Object(v)) }

// capn.JSON_enabled == false so we stub MarshallJSON().
func (s Z) MarshalJSON() (bs []byte, err error) { return }

type Z_List C.PointerList

func NewZList(s *C.Segment, sz int) Z_List { return Z_List(s.NewCompositeList(8, 1, sz)) }
func (s Z_List) Len() int                  { return C.PointerList(s).Len() }
func (s Z_List) At(i int) Z                { return Z(C.PointerList(s).At(i).ToStruct()) }
func (s Z_List) ToArray() []Z              { return *(*[]Z)(unsafe.Pointer(C.PointerList(s).ToArray())) }
func (s Z_List) Set(i int, item Z)         { C.PointerList(s).Set(i, C.Object(item)) }
