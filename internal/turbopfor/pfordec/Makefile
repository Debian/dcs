# Required for the Go compiler to intrinsify LZCNT, for example.
export GOAMD64=v3
# Required for SIMD support with Go 1.27.
# Always set (even before adding SIMD code) so the baseline is stable.
export GOEXPERIMENT=simd

GOTEST=go test -pgo=decode.cpuprof

# -count=6 gives p≤0.002 in benchstat:
# https://pkg.go.dev/golang.org/x/perf/cmd/benchstat
BENCHFLAGS=-run=^$$ -bench=. -benchtime=200000x -count=6
PERFBENCHFLAGS=-test.bench='Decode/n=2048/vals=debian-mix/impl=go$$' -test.benchtime=200000x

# use taskset -c1 to always pin to the same single core,
# avoiding accidental scheduling on different cores on
# mixed-core CPUs like the Ryzen 9 9950X3D.
TASKSET=taskset -c 1
BENCH=$(TASKSET) $(GOTEST) $(BENCHFLAGS)

.PHONY: all test bench bench-baseline bench-relative perf perf-per-value pgo-profile

all: bench

pgo-profile:
	$(TASKSET) go test -run=^$$ -bench='Decode/.*/.*/impl=go$$' -benchtime=3s -cpuprofile decode.cpuprof

test:
	$(GOTEST)

bench: test
	$(BENCH) | tee bench.txt
	benchstat -col /impl -row '/n /vals' -filter '.unit:(Mval/s)' bench.txt

bench-baseline: test
	$(BENCH) | tee baseline.txt

bench-relative: test
	$(BENCH) | tee bench.txt
	benchstat -filter '/impl:go .unit:(Mval/s)' baseline.txt bench.txt

# Use perf(1) to capture AMD IBS (the equivalent to Intel PEBS)
# PipelineL1 is roughly equivalent to Intel TopdownL1
perf:
	$(GOTEST) -c
	$(TASKSET) perf stat -M PipelineL1 ./pfordec.test -test.run=^$$ $(PERFBENCHFLAGS)
	sudo perf record -F 4999 -e ibs_op// --call-graph fp ./pfordec.test -test.run=^$$ $(PERFBENCHFLAGS)
	sudo chmod 644 perf.data

# 488281 iterations × 2048 values = 1.000e9 values, so counter/1e9 = per value.
perf-per-value:
	$(GOTEST) -c
	$(TASKSET) perf stat -x, -e cycles:u,instructions:u,branches:u,branch-misses:u ./pfordec.test -test.run=^$$ -test.bench='Decode/n=2048/vals=debian-mix/impl=go$$' -test.benchtime=488281x 2>&1 >/dev/null | awk -F, '{printf "%-16s %6.2f /val\n", $$3, $$1/1e9}'
