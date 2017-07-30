package godebiancontrol_test

import (
	"bytes"
	"github.com/stapelberg/godebiancontrol"
	"testing"
)

func TestParseSignedDsc(t *testing.T) {
	contents := bytes.NewBufferString(`-----BEGIN PGP SIGNED MESSAGE-----
Hash: SHA256

Format: 3.0 (quilt)
Source: aria2
Binary: aria2
Architecture: any
Version: 1.18.5-1
Maintainer: Patrick Ruckstuhl <patrick@ch.tario.org>
Uploaders: Kartik Mistry <kartik@debian.org>
Homepage: http://aria2.sourceforge.net/
Standards-Version: 3.9.5
Vcs-Browser: http://anonscm.debian.org/gitweb/?p=collab-maint/aria2.git;a=summary
Vcs-Git: git://anonscm.debian.org/collab-maint/aria2.git
Testsuite: autopkgtest
Build-Depends: autotools-dev, debhelper (>= 7), dpkg-dev (>= 1.16.1~), libc-ares-dev, libgcrypt11-dev (>= 1.5.0-3) | libgcrypt-dev, libgnutls28-dev, libsqlite3-dev, libxml2-dev, pkg-config, zlib1g-dev | libz-dev
Package-List: 
 aria2 deb net optional
Checksums-Sha1: 
 91639bf99a2e84873675f470fd36cee47f466770 2102797 aria2_1.18.5.orig.tar.bz2
 c031efb88a477986dac82477433ee0865643bf27 5428 aria2_1.18.5-1.debian.tar.xz
Checksums-Sha256: 
 25e21f94bb278a8624e0e4e131e740d105f7d5570290053beb8ae6c33fb9ce3f 2102797 aria2_1.18.5.orig.tar.bz2
 112aa6973779e9ebaf51d8ab445534fffad4562d4e2de3afd3352f3f3b2f6df3 5428 aria2_1.18.5-1.debian.tar.xz
Files: 
 79ddd76decadba7176b27c653f5c5aa2 2102797 aria2_1.18.5.orig.tar.bz2
 3f2a5585139c649765c6fc5db95bb32a 5428 aria2_1.18.5-1.debian.tar.xz

-----BEGIN PGP SIGNATURE-----
Version: GnuPG v1

iQIcBAEBCAAGBQJTRZTXAAoJEALB0/J4OqTeVlUQAJ0hkUIuf84ixkANGC51nGyW
weWeVg2l1ozkDTgSx4NpDaVGzWzmVVTMHByMLfGToDiuWOxHc6qCwtLLlGg7Qdg8
jbDfR21wUA//b+/Pt8SPUP3uAffQ4Rq7D65Cdr23Fkd9LJcOmgf8NkwRKcfXzsx6
ZWj9zK2RVNAwOjTDQGs7OEx2LZsFmL0mbO67ifCsuhWU9JJltf0VgRz5BwkXPnPw
V7Ouq0zE98w2B/Ssq+eRjw/25e7C+DV58lBWeCy+qH4yKigjz3tm9Y7WS9XVPHUa
EjC8mUzT6RhFLWCgtP0NDhgxX0lcm2MNp7iYV7IVdVq99cKsOBZvNXl+TS7v+tjr
JNEKVT4wMHzC0pdGjR2ly0AkF091u2ewrRfefO56q2LOjrRkzKi9smn7mqTfIx53
WpmQL+3ls27LQ6bwl+KeHuRRyj77TIKGyG/9ywyy3IIR4y7NM3wo9T3DQWHDhF6x
8mKG848AqSwFRNROT0gnW/hRIM6umZnhJT7xYhz3LgTnq+0UG2DldDiAcUzOD+S3
Jf6iv6b+hwO3+exs4sjJ1tzcIu2R7LroTjBn8zqZno5YeVzUcN9kRMHls13F0gtb
HwXGSPZ8O8m3ASS7XPpo+vmT5T/W0h75NvAAm7ju9V7EgpGJbE5RwVskYvIqoeif
U6LiZnj6CDeY9Xtjsi2l
=7fkT
-----END PGP SIGNATURE-----`)
	paragraphs, err := godebiancontrol.Parse(godebiancontrol.PGPSignatureStripper(contents))
	if err != nil {
		t.Fatal(err)
	}
	if len(paragraphs) != 1 {
		t.Fatal("Expected exactly one paragraphs")
	}
	if paragraphs[0]["Format"] != "3.0 (quilt)" {
		t.Fatal(`"Format" (simple) was not parsed correctly`)
	}
	if paragraphs[0]["Testsuite"] != "autopkgtest" {
		t.Fatal(`"Testsuite" was not parsed correctly`)
	}
}
