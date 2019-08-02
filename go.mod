module github.com/Debian/dcs

require (
	github.com/beorn7/perks v0.0.0-20180321164747-3a771d992973 // indirect
	github.com/codahale/hdrhistogram v0.0.0-20161010025455-3a0bb77429bd // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/gogo/protobuf v1.1.1 // indirect
	github.com/golang/protobuf v1.2.0
	github.com/google/codesearch v0.0.0-20150617151851-a45d81b686e8
	github.com/google/go-cmp v0.2.0
	github.com/google/renameio v0.0.0-20181127164028-8bac8552c408
	github.com/grpc-ecosystem/go-grpc-middleware v1.0.0
	github.com/kr/pretty v0.1.0
	github.com/matttproud/golang_protobuf_extensions v1.0.1 // indirect
	github.com/opentracing-contrib/go-stdlib v0.0.0-20181101210145-c9628a4f0148
	github.com/opentracing/opentracing-go v1.0.2
	github.com/pebbe/zmq4 v1.0.0
	github.com/pkg/errors v0.8.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_golang v0.9.1
	github.com/prometheus/client_model v0.0.0-20180712105110-5c3871d89910 // indirect
	github.com/prometheus/common v0.0.0-20181126121408-4724e9255275 // indirect
	github.com/prometheus/procfs v0.0.0-20181129180645-aa55a523dc0a // indirect
	github.com/stapelberg/godebiancontrol v0.0.0-20180408134423-8c93e189186a
	github.com/stretchr/testify v1.2.2 // indirect
	github.com/uber-go/atomic v1.3.2 // indirect
	github.com/uber/jaeger-client-go v2.15.0+incompatible
	github.com/uber/jaeger-lib v1.5.0 // indirect
	go.uber.org/atomic v1.3.2 // indirect
	golang.org/x/net v0.0.0-20181201002055-351d144fa1fc
	golang.org/x/sync v0.0.0-20181108010431-42b317875d0f
	golang.org/x/sys v0.0.0-20181128092732-4ed8d59d0b35
	golang.org/x/xerrors v0.0.0-20190212162355-a5947ffaace3
	google.golang.org/genproto v0.0.0-20181202183823-bd91e49a0898 // indirect
	google.golang.org/grpc v1.16.0
	pault.ag/go/debian v0.0.0-20180722221659-90aeb542bd40
)

replace github.com/stapelberg/goturbopfor => /home/michael/go/src/goturbopfor

go 1.13
