# vim:ts=4:sw=4:et
@0xf3d1bd40395eeea7;

using Go = import "/src/github.com/glycerine/go-capnproto/go.capnp";
$Go.package("proto");
$Go.import("/src/github.com/Debian/dcs/proto");

struct ProgressUpdate {
    filesprocessed @0 :UInt64;
    filestotal @1 :UInt64;
}
