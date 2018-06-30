package turbopfor

/*
#cgo CFLAGS: -g
#cgo LDFLAGS: ${SRCDIR}/vp4c.o ${SRCDIR}/vp4c_avx2.o ${SRCDIR}/vp4d.o ${SRCDIR}/vp4d_sse.o ${SRCDIR}/vp4d_avx2.o ${SRCDIR}/bitutil.o ${SRCDIR}/vint.o ${SRCDIR}/bitunpack.o ${SRCDIR}/bitunpack_sse.o ${SRCDIR}/bitunpack_avx2.o ${SRCDIR}/bitpack.o ${SRCDIR}/bitpack_avx2.o -lm
#include "conf.h"
#include "bitpack.h"
#include "vp4.h"

static unsigned myp4enc32(uint32_t *__restrict in, unsigned n,
                       unsigned char *__restrict out) {
  unsigned char *endptr = p4enc32(in, n, out);
  return endptr - out;
}

static unsigned myp4enc256v32(uint32_t *__restrict in, unsigned n,
                       unsigned char *__restrict out) {
  unsigned char *endptr = p4enc256v32(in, n, out);
  return endptr - out;
}

static unsigned myp4dec32(unsigned char *__restrict in,
                              unsigned n,
                              uint32_t *__restrict out) {
  unsigned char *endptr = p4dec32(in, n, out);
  return endptr - in;
}

static unsigned myp4dec256v32(unsigned char *__restrict in,
                              unsigned n,
                              uint32_t *__restrict out) {
  unsigned char *endptr = p4dec256v32(in, n, out);
  return endptr - in;
}


static int minlen(unsigned cnt) {
  // (((bit64(array, cnt) + 7) / 8) * SIZE_ROUNDUP(cnt, 32)) + 1
  return (((32 + 7) / 8) * SIZE_ROUNDUP(cnt, 32)) + 1;
}
*/
import "C"

// KNOWN WORKING
// len(input) % 32 must be 0 (pad with 0x00 if necessary).
func P4nenc32(input []uint32) []byte {
	n := len(input)
	buffer := make([]byte, ((n+127)/128)+(n+32)*4)
	num := C.p4nenc32((*C.uint32_t)(&input[0]),
		C.size_t(len(input)),
		(*C.uchar)(&buffer[0]))
	return buffer[:int(num)]
}

// len(input) % 32 must be 0 (pad with 0x00 if necessary).
func P4enc32(input []uint32) []byte {
	n := len(input)
	buffer := make([]byte, ((n+127)/128)+(n+32)*4)
	num := C.myp4enc32((*C.uint32_t)(&input[0]),
		C.unsigned(len(input)),
		(*C.uchar)(&buffer[0]))
	return buffer[:int(num)]
}

// len(input) % 32 must be 0 (pad with 0x00 if necessary).
func P4enc256v32(input []uint32) []byte {
	n := len(input)
	buffer := make([]byte, ((n+127)/128)+(n+32)*4)
	num := C.myp4enc256v32((*C.uint32_t)(&input[0]),
		C.unsigned(len(input)),
		(*C.uchar)(&buffer[0]))
	return buffer[:int(num)]
}

// len(input) % 32 must be 0 (pad with 0x00 if necessary).
func P4nenc256v32(input []uint32) []byte {
	n := len(input)
	buffer := make([]byte, ((n+127)/128)+(n+32)*4)
	num := C.p4nenc256v32((*C.uint32_t)(&input[0]),
		C.size_t(len(input)),
		(*C.uchar)(&buffer[0]))
	return buffer[:int(num)]
}

// len(input) % 32 must be 0 (pad with 0x00 if necessary).
func P4nzenc32(input []uint32) []byte {
	n := len(input)
	buffer := make([]byte, ((n+127)/128)+(n+32)*4)
	num := C.p4nzenc32((*C.uint32_t)(&input[0]),
		C.size_t(len(input)),
		(*C.uchar)(&buffer[0]))
	return buffer[:int(num)]
}

// len(input) % 32 must be 0 (pad with 0x00 if necessary).
func P4nd1enc32(input []uint32) []byte {
	n := len(input)
	buffer := make([]byte, ((n+127)/128)+(n+32)*4)
	num := C.p4nd1enc32((*C.uint32_t)(&input[0]),
		C.size_t(len(input)),
		(*C.uchar)(&buffer[0]))
	return buffer[:int(num)]
}

func P4dec32(input []byte, output []uint32) (read int) {
	return int(C.myp4dec32((*C.uchar)(&input[0]),
		C.unsigned(len(output)),
		(*C.uint32_t)(&output[0])))
}

func P4ndec32(input []byte, output []uint32) (read int) {
	return int(C.p4ndec32((*C.uchar)(&input[0]),
		C.size_t(len(output)),
		(*C.uint32_t)(&output[0])))
}

func P4dec256v32(input []byte, output []uint32) (read int) {
	return int(C.myp4dec256v32((*C.uchar)(&input[0]),
		C.unsigned(len(output)),
		(*C.uint32_t)(&output[0])))
}

func P4ndec256v32(input []byte, output []uint32) (read int) {
	return int(C.p4ndec256v32((*C.uchar)(&input[0]),
		C.size_t(len(output)),
		(*C.uint32_t)(&output[0])))
}

func P4nd1dec32(input []byte, output []uint32) (read int) {
	return int(C.p4nd1dec32((*C.uchar)(&input[0]),
		C.size_t(len(output)),
		(*C.uint32_t)(&output[0])))
}
