package turbopfor

/*
#cgo CFLAGS: -g
#cgo LDFLAGS: -lm
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

// Corresponding to p4nbound256v32:
//
// #define VP4BOUND(_n_, _esize_, _csize_) ((_esize_*_n_) + ((_n_+_csize_-1)/_csize_))
// size_t p4nbound256v32(size_t n) { return VP4BOUND(n, 4, 256); }
//
// see also https://github.com/powturbo/TurboPFor-Integer-Compression/issues/59
func EncodingSize(n int) int {
	return ((n + 255) / 256) + (n+32)*4
}

// Corresponding to p4nbound32:
//
// #define VP4BOUND(_n_, _esize_, _csize_) ((_esize_*_n_) + ((_n_+_csize_-1)/_csize_))
// size_t p4nbound32(    size_t n) { return VP4BOUND(n, 4, 128); }
//
// see also https://github.com/powturbo/TurboPFor-Integer-Compression/issues/59
//
// see also https://github.com/powturbo/TurboPFor-Integer-Compression/issues/84
func DecodingSize(n int) int {
	return ((n + 127) / 128) + (n+32)*4
}

// KNOWN WORKING
// len(input) % 32 must be 0 (pad with 0x00 if necessary).
func P4nenc32(input []uint32) []byte {
	buffer := make([]byte, EncodingSize(len(input)))
	num := C.p4nenc32((*C.uint32_t)(&input[0]),
		C.size_t(len(input)),
		(*C.uchar)(&buffer[0]))
	return buffer[:int(num)]
}

// len(input) % 32 must be 0 (pad with 0x00 if necessary).
func P4enc32(input []uint32) []byte {
	buffer := make([]byte, EncodingSize(len(input)))
	num := C.myp4enc32((*C.uint32_t)(&input[0]),
		C.unsigned(len(input)),
		(*C.uchar)(&buffer[0]))
	return buffer[:int(num)]
}

// len(input) % 32 must be 0 (pad with 0x00 if necessary).
func P4enc256v32(input []uint32, output []byte) int {
	num := C.myp4enc256v32((*C.uint32_t)(&input[0]),
		C.unsigned(len(input)),
		(*C.uchar)(&output[0]))
	return int(num)
}

// len(input) % 32 must be 0 (pad with 0x00 if necessary).
func P4nenc256v32Buf(buffer []byte, input []uint32) []byte {
	num := C.p4nenc256v32((*C.uint32_t)(&input[0]),
		C.size_t(len(input)),
		(*C.uchar)(&buffer[0]))
	return buffer[:int(num)]
}

// len(input) % 32 must be 0 (pad with 0x00 if necessary).
func P4nenc256v32(input []uint32) []byte {
	buffer := make([]byte, EncodingSize(len(input)))
	return P4nenc256v32Buf(buffer, input)
}

// len(input) % 32 must be 0 (pad with 0x00 if necessary).
func P4nzenc32(input []uint32) []byte {
	buffer := make([]byte, EncodingSize(len(input)))
	num := C.p4nzenc32((*C.uint32_t)(&input[0]),
		C.size_t(len(input)),
		(*C.uchar)(&buffer[0]))
	return buffer[:int(num)]
}

// len(input) % 32 must be 0 (pad with 0x00 if necessary).
func P4nd1enc32(input []uint32) []byte {
	buffer := make([]byte, EncodingSize(len(input)))
	num := C.p4nd1enc32((*C.uint32_t)(&input[0]),
		C.size_t(len(input)),
		(*C.uchar)(&buffer[0]))
	return buffer[:int(num)]
}

func P4dec32(input []byte, output []uint32) (read int) {
	// TurboPFor (at least older versions? see
	// https://github.com/powturbo/TurboPFor-Integer-Compression/issues/84) read
	// past their input buffer, so verify that the input buffer has enough
	// capacity and use a temporary buffer if needed:
	if required := DecodingSize(len(output)); cap(input) < required {
		buffer := make([]byte, len(input), required)
		copy(buffer, input)
		input = buffer
	}
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
