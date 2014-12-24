# vim:ts=4:sw=4:et
@0xd7ab5d5e833e9e61;

# compile with: capnp compile -I $GOPATH -ogo match.capnp
# TODO: can we use “go generate” for this?

using Go = import "/src/github.com/glycerine/go-capnproto/go.capnp";
$Go.package("proto");
$Go.import("/src/github.com/Debian/dcs/proto");

struct Match {
    path @0 :Text;
    line @1 :UInt32;

    # Contents of line-2.
    ctxp2 @2 :Text;
    # Contents of line-1.
    ctxp1 @3 :Text;
    # Contents of the line containing the match.
    context @4 :Text;
    # Contents of line+1.
    ctxn1 @5 :Text;
    # Contents of line+2.
    ctxn2 @6 :Text;

    pathrank @7 :Float32;
    ranking @8 :Float32;
    package @9 :Text;
}
