package proto

// AUTO GENERATED - DO NOT EDIT

import (
	"bufio"
	"bytes"
	"encoding/json"
	C "github.com/glycerine/go-capnproto"
	"io"
	"math"
	"unsafe"
)

type Match C.Struct

func NewMatch(s *C.Segment) Match      { return Match(s.NewStruct(16, 7)) }
func NewRootMatch(s *C.Segment) Match  { return Match(s.NewRootStruct(16, 7)) }
func AutoNewMatch(s *C.Segment) Match  { return Match(s.NewStructAR(16, 7)) }
func ReadRootMatch(s *C.Segment) Match { return Match(s.Root(0).ToStruct()) }
func (s Match) Path() string           { return C.Struct(s).GetObject(0).ToText() }
func (s Match) SetPath(v string)       { C.Struct(s).SetObject(0, s.Segment.NewText(v)) }
func (s Match) Line() uint32           { return C.Struct(s).Get32(0) }
func (s Match) SetLine(v uint32)       { C.Struct(s).Set32(0, v) }
func (s Match) Ctxp2() string          { return C.Struct(s).GetObject(1).ToText() }
func (s Match) SetCtxp2(v string)      { C.Struct(s).SetObject(1, s.Segment.NewText(v)) }
func (s Match) Ctxp1() string          { return C.Struct(s).GetObject(2).ToText() }
func (s Match) SetCtxp1(v string)      { C.Struct(s).SetObject(2, s.Segment.NewText(v)) }
func (s Match) Context() string        { return C.Struct(s).GetObject(3).ToText() }
func (s Match) SetContext(v string)    { C.Struct(s).SetObject(3, s.Segment.NewText(v)) }
func (s Match) Ctxn1() string          { return C.Struct(s).GetObject(4).ToText() }
func (s Match) SetCtxn1(v string)      { C.Struct(s).SetObject(4, s.Segment.NewText(v)) }
func (s Match) Ctxn2() string          { return C.Struct(s).GetObject(5).ToText() }
func (s Match) SetCtxn2(v string)      { C.Struct(s).SetObject(5, s.Segment.NewText(v)) }
func (s Match) Pathrank() float32      { return math.Float32frombits(C.Struct(s).Get32(4)) }
func (s Match) SetPathrank(v float32)  { C.Struct(s).Set32(4, math.Float32bits(v)) }
func (s Match) Ranking() float32       { return math.Float32frombits(C.Struct(s).Get32(8)) }
func (s Match) SetRanking(v float32)   { C.Struct(s).Set32(8, math.Float32bits(v)) }
func (s Match) Package() string        { return C.Struct(s).GetObject(6).ToText() }
func (s Match) SetPackage(v string)    { C.Struct(s).SetObject(6, s.Segment.NewText(v)) }
func (s Match) WriteJSON(w io.Writer) error {
	b := bufio.NewWriter(w)
	var err error
	var buf []byte
	_ = buf
	err = b.WriteByte('{')
	if err != nil {
		return err
	}

	_, err = b.WriteString("\"Type\":\"result\",")
	if err != nil {
		return err
	}

	_, err = b.WriteString("\"Path\":")
	if err != nil {
		return err
	}
	{
		s := s.Path()
		buf, err = json.Marshal(s)
		if err != nil {
			return err
		}
		_, err = b.Write(buf)
		if err != nil {
			return err
		}
	}
	err = b.WriteByte(',')
	if err != nil {
		return err
	}
	_, err = b.WriteString("\"Line\":")
	if err != nil {
		return err
	}
	{
		s := s.Line()
		buf, err = json.Marshal(s)
		if err != nil {
			return err
		}
		_, err = b.Write(buf)
		if err != nil {
			return err
		}
	}
	err = b.WriteByte(',')
	if err != nil {
		return err
	}
	_, err = b.WriteString("\"Ctxp2\":")
	if err != nil {
		return err
	}
	{
		s := s.Ctxp2()
		buf, err = json.Marshal(s)
		if err != nil {
			return err
		}
		_, err = b.Write(buf)
		if err != nil {
			return err
		}
	}
	err = b.WriteByte(',')
	if err != nil {
		return err
	}
	_, err = b.WriteString("\"Ctxp1\":")
	if err != nil {
		return err
	}
	{
		s := s.Ctxp1()
		buf, err = json.Marshal(s)
		if err != nil {
			return err
		}
		_, err = b.Write(buf)
		if err != nil {
			return err
		}
	}
	err = b.WriteByte(',')
	if err != nil {
		return err
	}
	_, err = b.WriteString("\"Context\":")
	if err != nil {
		return err
	}
	{
		s := s.Context()
		buf, err = json.Marshal(s)
		if err != nil {
			return err
		}
		_, err = b.Write(buf)
		if err != nil {
			return err
		}
	}
	err = b.WriteByte(',')
	if err != nil {
		return err
	}
	_, err = b.WriteString("\"Ctxn1\":")
	if err != nil {
		return err
	}
	{
		s := s.Ctxn1()
		buf, err = json.Marshal(s)
		if err != nil {
			return err
		}
		_, err = b.Write(buf)
		if err != nil {
			return err
		}
	}
	err = b.WriteByte(',')
	if err != nil {
		return err
	}
	_, err = b.WriteString("\"Ctxn2\":")
	if err != nil {
		return err
	}
	{
		s := s.Ctxn2()
		buf, err = json.Marshal(s)
		if err != nil {
			return err
		}
		_, err = b.Write(buf)
		if err != nil {
			return err
		}
	}
	err = b.WriteByte(',')
	if err != nil {
		return err
	}
	_, err = b.WriteString("\"PathRank\":")
	if err != nil {
		return err
	}
	{
		s := s.Pathrank()
		buf, err = json.Marshal(s)
		if err != nil {
			return err
		}
		_, err = b.Write(buf)
		if err != nil {
			return err
		}
	}
	err = b.WriteByte(',')
	if err != nil {
		return err
	}
	_, err = b.WriteString("\"Ranking\":")
	if err != nil {
		return err
	}
	{
		s := s.Ranking()
		buf, err = json.Marshal(s)
		if err != nil {
			return err
		}
		_, err = b.Write(buf)
		if err != nil {
			return err
		}
	}
	err = b.WriteByte(',')
	if err != nil {
		return err
	}
	_, err = b.WriteString("\"Package\":")
	if err != nil {
		return err
	}
	{
		s := s.Package()
		buf, err = json.Marshal(s)
		if err != nil {
			return err
		}
		_, err = b.Write(buf)
		if err != nil {
			return err
		}
	}
	err = b.WriteByte('}')
	if err != nil {
		return err
	}
	err = b.Flush()
	return err
}
func (s Match) MarshalJSON() ([]byte, error) {
	b := bytes.Buffer{}
	err := s.WriteJSON(&b)
	return b.Bytes(), err
}

type Match_List C.PointerList

func NewMatchList(s *C.Segment, sz int) Match_List { return Match_List(s.NewCompositeList(16, 7, sz)) }
func (s Match_List) Len() int                      { return C.PointerList(s).Len() }
func (s Match_List) At(i int) Match                { return Match(C.PointerList(s).At(i).ToStruct()) }
func (s Match_List) ToArray() []Match              { return *(*[]Match)(unsafe.Pointer(C.PointerList(s).ToArray())) }
func (s Match_List) Set(i int, item Match)         { C.PointerList(s).Set(i, C.Object(item)) }
