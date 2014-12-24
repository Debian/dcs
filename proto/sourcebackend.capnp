# vim:ts=4:sw=4:et
@0xdb3664d4a8c6e745;

using Go = import "/src/github.com/glycerine/go-capnproto/go.capnp";
$Go.package("proto");
$Go.import("/src/github.com/Debian/dcs/proto");

using P = import "progressupdate.capnp";
using M = import "match.capnp";

struct Z {
    union {
        progressupdate @0 :P.ProgressUpdate;
        match @1 :M.Match;
    }
}
