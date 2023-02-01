FROM debian:bookworm

RUN apt-get update && apt-get install -y git ca-certificates build-essential

RUN git clone https://github.com/powturbo/TurboPFor-Integer-Compression
WORKDIR TurboPFor-Integer-Compression
RUN git reset --hard 41dfc01bf24bb58ac4c4b3e62a2a548d0915bc95

# These fixes from pps83 also don’t compile on Linux:
# https://github.com/powturbo/TurboPFor-Integer-Compression/pull/82#issuecomment-1412710789
#RUN git clone -b masterz https://github.com/pps83/TurboPFor
#WORKDIR TurboPFor

RUN make AVX2=1

# The turbopfor.syso in .. was generated on an Intel i9-9900K using:
# docker build --rm -t turbopfor .
# docker run --rm -v $PWD:/tmp/gokrazy/ turbopfor sh -c 'ld -r bitpack_avx2.o bitpack.o bitunpack_avx2.o bitunpack.o bitunpack_sse.o bitutil.o vint.o vp4c_avx2.o vp4c.o vp4d_avx2.o vp4d.o vp4d_sse.o -o /tmp/gokrazy/turbopfor.syso'
