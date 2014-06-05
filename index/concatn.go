package index

import (
	"container/heap"
	// TODO: use container/vector as base for concatHeap
	// TODO: or maybe we can use an in-place heap? in pprof top10, one can see memmove and garbage collection from push/pull to be major factors
	//"container/vector"
	"os"
)

type concatHeap []postMapReader

func (h *concatHeap) Less(i, j int) bool {
	if (*h)[i].trigram == (*h)[j].trigram {
		return (*h)[i].idmap[0].new < (*h)[j].idmap[0].new
	}
	return (*h)[i].trigram < (*h)[j].trigram
}

func (h *concatHeap) Swap(i, j int) {
	(*h)[i], (*h)[j] = (*h)[j], (*h)[i]
}

func (h *concatHeap) Len() int {
	return len(*h)
}

func (h *concatHeap) Pop() (v interface{}) {
	*h, v = (*h)[:h.Len()-1], (*h)[h.Len()-1]
	return
}

func (h *concatHeap) Push(v interface{}) {
	*h = append(*h, v.(postMapReader))
}

//type concatHeap struct {
//	vector.Vector
//}
//
//func (h *concatHeap) Less(i, j int) bool {
//	return h.At(i).(postMapReader).trigram < h.At(j).(postMapReader).trigram
//}

func ConcatN(dst string, sources ...string) {
	//offsets := make([]uint32, len(sources))
	ixes := make([]*Index, len(sources))
	readers := make([]postMapReader, len(sources))
	for i, source := range sources {
		ixes[i] = Open(source)
	}

	out := bufCreate(dst)
	out.writeString(magic)

	// Merged list of paths.
	pathData := out.offset()
	out.writeString("\x00")

	// Merged list of names.
	nameData := out.offset()
	nameIndexFile := bufCreate("")
	var offset uint32
	for i, _ := range sources {
		readers[i].init(ixes[i], []idrange{{
			lo:  0,
			hi:  uint32(ixes[i].numName),
			new: offset}})
		offset += uint32(ixes[i].numName)
		// TODO: we can just memcpy the blocks of names, but we still need to
		// fix up all the nameIndexFile numbers (i.e. write out.offset() + num
		// instead of num). That could be faster than the following code, though:
		for j := 0; j < ixes[i].numName; j++ {
			nameIndexFile.writeUint32(out.offset() - nameData)
			out.writeString(ixes[i].Name(uint32(j)))
			out.writeString("\x00")
		}
	}

	nameIndexFile.writeUint32(out.offset())

	// Merged list of posting lists.
	postData := out.offset()
	var w postDataWriter

	w.init(out)

	h := new(concatHeap)
	lastTrigram := ^uint32(0)
	for i, _ := range sources {
		heap.Push(h, readers[i])
	}
	for {
		reader := heap.Pop(h).(postMapReader)
		nextTrigram := reader.trigram

		if nextTrigram == ^uint32(0) {
			break
		}

		if lastTrigram != nextTrigram && lastTrigram != ^uint32(0) {
			w.endTrigram()
		}
		if lastTrigram != nextTrigram {
			w.trigram(nextTrigram)
		}

		reader.writePostingList(&w)
		reader.nextTrigram()
		heap.Push(h, reader)

		lastTrigram = nextTrigram
	}

	// Name index
	nameIndex := out.offset()
	copyFile(out, nameIndexFile)

	// Posting list index
	postIndex := out.offset()
	copyFile(out, w.postIndexFile)

	out.writeUint32(pathData)
	out.writeUint32(nameData)
	out.writeUint32(postData)
	out.writeUint32(nameIndex)
	out.writeUint32(postIndex)
	out.writeString(trailerMagic)
	out.flush()

	os.Remove(nameIndexFile.name)
	os.Remove(w.postIndexFile.name)
}
