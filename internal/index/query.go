package index

import (
	"log"
	"sort"

	"github.com/Debian/dcs/index"
)

func (i *Index) PostingQuery(q *index.Query) (docids []uint32) {
	return i.postingQuery(q, nil)
}

// Implements sort.Interface
type trigramCnt struct {
	trigram uint32
	count   int
	listcnt int
}

type trigramCnts []trigramCnt

func (t trigramCnts) Len() int {
	return len(t)
}

func (t trigramCnts) Less(i, j int) bool {
	return t[i].count < t[j].count
}

func (t trigramCnts) Swap(i, j int) {
	t[i], t[j] = t[j], t[i]
}

func (ix *Index) postingQuery(qry *index.Query, restrict []uint32) (ret []uint32) {
	var list []uint32
	switch qry.Op {
	case index.QNone:
		// nothing
	case index.QAll:
		if restrict != nil {
			return restrict
		}
		list = make([]uint32, ix.DocidMap.Count)
		for i := range list {
			list[i] = uint32(i)
		}
		return list
	case index.QAnd:
		// "Query planner": we first sort the posting lists by their
		// length (ascending)
		withCount := make(trigramCnts, 0, len(qry.Trigram))
		for _, t := range qry.Trigram {
			tri := uint32(t[0])<<16 | uint32(t[1])<<8 | uint32(t[2])
			if tri == 2105376 {
				continue // skip "   " for now
			}
			meta, _, err := ix.Docid.metaEntry(Trigram(tri))
			if err != nil {
				log.Printf("numEntries: (probably okay) tri %d (%v): %v", tri, t, err)
				//continue
			}
			//withCount[idx] = trigramCnt{tri, int(count), 0}
			withCount = append(withCount, trigramCnt{tri, int(meta.Entries), 0})
		}
		sort.Sort(withCount)
		if len(withCount) > 0 {
			//q.pfdocid.growBuffer(withCount[len(withCount)-1].count)
		}

		stoppedAt := 0
		for idx, t := range withCount {
			previous := len(list)
			if list == nil {
				list = ix.postingList(t.trigram, restrict)
			} else {
				list = ix.postingAnd(list, t.trigram, restrict)
			}
			if len(list) == 0 {
				return nil
			}
			//continue
			withCount[idx].listcnt = len(list)
			if previous > 0 {
				minIdx := 0.70 * float32(len(withCount))
				if (previous-len(list)) < 10 && stoppedAt == 0 && float32(idx) > minIdx {
					stoppedAt = len(list)
				}
			}
			if previous > 0 && (previous-len(list)) < 10 {
				//fmt.Printf("difference is %d, break!\n", previous - len(list))
				break
			}
		}

		for _, sub := range qry.Sub {
			if list == nil {
				list = restrict
			}
			list = ix.postingQuery(sub, list)
			if len(list) == 0 {
				return nil
			}
		}
	case index.QOr:
		for _, t := range qry.Trigram {
			tri := uint32(t[0])<<16 | uint32(t[1])<<8 | uint32(t[2])
			if list == nil {
				list = ix.postingList(tri, restrict)
			} else {
				list = ix.postingOr(list, tri, restrict)
			}
		}
		for _, sub := range qry.Sub {
			list1 := ix.postingQuery(sub, restrict)
			list = mergeOr(list, list1)
		}
	}
	return list
}

func mergeOr(l1, l2 []uint32) []uint32 {
	var l []uint32
	i := 0
	j := 0
	for i < len(l1) || j < len(l2) {
		switch {
		case j == len(l2) || (i < len(l1) && l1[i] < l2[j]):
			l = append(l, l1[i])
			i++
		case i == len(l1) || (j < len(l2) && l1[i] > l2[j]):
			l = append(l, l2[j])
			j++
		case l1[i] == l2[j]:
			l = append(l, l1[i])
			i++
			j++
		}
	}
	return l
}

func (ix *Index) readDocids(t Trigram, restrict []uint32) ([]uint32, error) {
	docids, err := ix.Docid.Deltas(t)
	if err != nil {
		return nil, err
	}
	var prev uint32
	entries := make([]uint32, len(docids))
	//var entries []uint32
	if len(docids) == 0 {
		return nil, nil
	}
	var oidx int
	for _, docid := range docids {
		docid += prev
		prev = docid
		if restrict != nil {
			i := 0
			for i < len(restrict) && restrict[i] < docid {
				i++
			}
			restrict = restrict[i:]
			if len(restrict) == 0 || restrict[0] != docid {
				continue
			}
		}
		entries[oidx] = docid
		oidx++
	}
	return entries[:oidx], nil
}

func (ix *Index) postingList(tri uint32, restrict []uint32) []uint32 {
	x, err := ix.readDocids(Trigram(tri), restrict)
	if err != nil {
		log.Printf("(probably okay?) tri %d: %v", tri, err)
	}
	//log.Printf("len(postingList(%d, restrict %d)) = %d", tri, len(restrict), len(x))
	return x
}

func (ix *Index) postingAnd(list []uint32, tri uint32, restrict []uint32) []uint32 {
	l, err := ix.readDocids(Trigram(tri), restrict)
	if err != nil {
		log.Printf("(probably okay?) tri %d: %v", tri, err)
	}
	x := make([]uint32, 0, len(l))
	i := 0
	for _, fileid := range l {
		for i < len(list) && list[i] < fileid {
			i++
		}
		if i < len(list) && list[i] == fileid {
			x = append(x, fileid)
			i++
		}
	}
	//log.Printf("len(postingAnd(%d, restrict %d)) = %d", tri, len(restrict), len(x))
	return x
}

func (ix *Index) postingOr(list []uint32, tri uint32, restrict []uint32) []uint32 {
	l, err := ix.readDocids(Trigram(tri), restrict)
	if err != nil {
		log.Printf("(probably okay?) tri %d: %v", tri, err)
	}
	//x := make([]uint32, 0, len(l))
	x := make([]uint32, len(l)+len(list))
	i := 0
	xn := 0
	for _, fileid := range l {
		for i < len(list) && list[i] < fileid {
			x[xn] = list[i]
			xn++
			//x = append(x, list[i])
			i++
		}
		x[xn] = fileid
		xn++
		//x = append(x, fileid)
		if i < len(list) && list[i] == fileid {
			i++
		}
	}
	copy(x[xn:], list[i:])
	xn += len(list[i:])
	//x = append(x, list[i:]...)
	//log.Printf("len(postingOr(%d, retrict %d)) = %d", tri, len(restrict), len(x))
	return x[:xn]
}
