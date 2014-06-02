// vim:ts=4:sw=4:noet
package index

/*
#cgo CFLAGS: -std=gnu99 -O3
#include <stdlib.h>
#include <stdio.h>
#include <stdint.h>

static __attribute__((hot)) const uint32_t uvarint(const uint8_t *restrict*data) {
    uint32_t b, c, d;
    if ((b = *((*data)++)) < 0x80) {
        return b;
    } else if ((c = *((*data)++)) < 0x80) {
        return (uint32_t) (b & 0x7F)        |
		       (uint32_t) (c << 7);
    } else if ((d = *((*data)++)) < 0x80) {
		return (uint32_t) (b & 0x7F)        |
		       (uint32_t)((c & 0x7F) << 7)  |
			   (uint32_t) (d << 14);
	} else {
		return (uint32_t) (b & 0x7F)        |
		       (uint32_t)((c & 0x7F) << 7)  |
			   (uint32_t)((d & 0x7F) << 14) |
			   ((uint32_t)(*((*data)++)) << 21);
	}
}

// NB: This is a fragile macro, beware! We cannot use the do { } while (0)
// trick because we use continue in the macro.
#define skip_unless_fileid_in(cntvar, lstvar) \
	while (cntvar && *lstvar < fileid) { \
		cntvar--; \
		lstvar++; \
	} \
	if (!cntvar || *lstvar != fileid) \
		continue;

#define RESTRICT \
	if (restrict_list) { \
		skip_unless_fileid_in(restrictcount, restrict_list); \
	}


int cPostingList(const uint8_t *restrict list,
                 int count,
                 uint32_t *restrict result,
                 int restrictcount,
                 uint32_t *restrict restrict_list) {
	int oidx = 0;
	int fileid = ~0;
	while (count--) {
		fileid += uvarint(&list);
		RESTRICT;
		result[oidx++] = fileid;
	}
	return oidx;
}

int cPostingAnd(const uint8_t *restrict list,
                int count,
				uint32_t *restrict file_id_list,
				int file_id_count,
				uint32_t *restrict result,
				int restrictcount,
				uint32_t *restrict restrict_list) {
	int oidx = 0;
	int fileid = ~0;
	while (count--) {
		fileid += uvarint(&list);
		RESTRICT;
		skip_unless_fileid_in(file_id_count, file_id_list);
		result[oidx++] = fileid;
	}
	return oidx;
}

int cPostingOr(const uint8_t *restrict list,
               int count,
			   uint32_t *restrict file_id_list,
			   int file_id_count,
			   uint32_t *restrict result,
			   int restrictcount,
			   uint32_t *restrict restrict_list) {
	int fidx = 0;
	int oidx = 0;
	int fileid = ~0;
	while (count--) {
		fileid += uvarint(&list);
		RESTRICT;
		while (fidx < file_id_count && file_id_list[fidx] < fileid)
			result[oidx++] = file_id_list[fidx++];
		result[oidx++] = fileid;
		if (fidx < file_id_count && file_id_list[fidx] == fileid)
			fidx++;
	}
	while (fidx < file_id_count)
		result[oidx++] = file_id_list[fidx++];
	return oidx;
}

int cPostingLast(const uint8_t *restrict list,
                 uint32_t count,
                 uint32_t fileid,
                 uint32_t *totalbytes) {
    const uint8_t *restrict start = list;
    while (count--) {
        fileid += uvarint(&list);
    }
    *totalbytes = (list - start);
    return fileid;
}

*/
import "C"

func myPostingList(data []byte, count int, restrict []uint32) []uint32 {
	result := make([]uint32, count)
	if count == 0 {
		return result
	}
	var restrictptr *C.uint32_t
	if len(restrict) > 0 {
		restrictptr = (*C.uint32_t)(&restrict[0])
	}
	num := C.cPostingList((*C.uint8_t)(&data[0]),
		C.int(count),
		(*C.uint32_t)(&result[0]),
		C.int(len(restrict)),
		restrictptr)
	return result[0:int(num)]
}

func myPostingAnd(data []byte, count int, list []uint32, restrict []uint32) []uint32 {
	// reverse enough space to hold it all, we truncate later
	result := make([]uint32, count)
	if count == 0 {
		return result
	}
	var listptr, restrictptr *C.uint32_t
	if len(restrict) > 0 {
		restrictptr = (*C.uint32_t)(&restrict[0])
	}
	if len(list) > 0 {
		listptr = (*C.uint32_t)(&list[0])
	}
	num := C.cPostingAnd((*C.uint8_t)(&data[0]),
		C.int(count),
		listptr,
		C.int(len(list)),
		(*C.uint32_t)(&result[0]),
		C.int(len(restrict)),
		restrictptr)
	return result[0:int(num)]
}

func myPostingOr(data []byte, count int, list []uint32, restrict []uint32) []uint32 {
	// reverse enough space to hold it all, we truncate later
	result := make([]uint32, len(list)+count)
	var dataptr *C.uint8_t
	var listptr, restrictptr *C.uint32_t
	if count > 0 {
		dataptr = (*C.uint8_t)(&data[0])
	}
	if len(restrict) > 0 {
		restrictptr = (*C.uint32_t)(&restrict[0])
	}
	if len(list) > 0 {
		listptr = (*C.uint32_t)(&list[0])
	}
	num := C.cPostingOr(dataptr,
		C.int(count),
		listptr,
		C.int(len(list)),
		(*C.uint32_t)(&result[0]),
		C.int(len(restrict)),
		restrictptr)
	return result[0:int(num)]
}

func myPostingLast(data []byte, count uint32, fileid uint32) (int, uint32) {
	var dataptr *C.uint8_t
	if count > 0 {
		dataptr = (*C.uint8_t)(&data[0])
	}
	var totalbytes uint32
	last := int(C.cPostingLast(dataptr,
		C.uint32_t(count),
		C.uint32_t(fileid),
		(*C.uint32_t)(&totalbytes)))
	return last, totalbytes
}
