module github.com/Debian/dcs

require (
	github.com/antihax/optional v1.0.0
	github.com/beorn7/perks v0.0.0-20180321164747-3a771d992973 // indirect
	github.com/coreos/go-oidc v2.2.1+incompatible
	github.com/edsrzf/mmap-go v1.0.0
	github.com/google/codesearch v1.2.0
	github.com/google/go-cmp v0.5.9
	github.com/google/renameio v0.0.0-20181127164028-8bac8552c408
	github.com/gorilla/securecookie v1.1.1
	github.com/grpc-ecosystem/go-grpc-middleware v1.1.0
	github.com/matttproud/golang_protobuf_extensions v1.0.1 // indirect
	github.com/pquerna/cachecontrol v0.0.0-20200819021114-67c6ae64274f // indirect
	github.com/prometheus/client_golang v0.9.1
	github.com/prometheus/common v0.0.0-20181126121408-4724e9255275 // indirect
	github.com/prometheus/procfs v0.0.0-20181129180645-aa55a523dc0a // indirect
	github.com/stapelberg/godebiancontrol v0.0.0-20180408134423-8c93e189186a
	golang.org/x/net v0.0.0-20201021035429-f5854403a974
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
	golang.org/x/sys v0.0.0-20210119212857-b64e53b001e4
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1
	google.golang.org/grpc v1.50.1
	google.golang.org/protobuf v1.27.1
	gopkg.in/square/go-jose.v2 v2.5.1 // indirect
	pault.ag/go/debian v0.0.0-20180722221659-90aeb542bd40
)

replace github.com/stapelberg/goturbopfor => /home/michael/go/src/goturbopfor

go 1.13
