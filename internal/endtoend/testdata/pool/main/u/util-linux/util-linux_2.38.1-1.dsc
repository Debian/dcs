-----BEGIN PGP SIGNED MESSAGE-----
Hash: SHA256

Format: 3.0 (quilt)
Source: util-linux
Binary: util-linux, util-linux-locales, mount, bsdutils, bsdextrautils, fdisk, fdisk-udeb, libblkid1, libblkid1-udeb, libblkid-dev, libfdisk1, libfdisk1-udeb, libfdisk-dev, libmount1, libmount1-udeb, libmount-dev, libsmartcols1, libsmartcols1-udeb, libsmartcols-dev, libuuid1, uuid-runtime, libuuid1-udeb, uuid-dev, util-linux-udeb, rfkill, eject, eject-udeb, util-linux-extra
Architecture: any all
Version: 2.38.1-1
Maintainer: util-linux packagers <util-linux@packages.debian.org>
Uploaders: Chris Hofstaedtler <zeha@debian.org>
Homepage: https://www.kernel.org/pub/linux/utils/util-linux/
Standards-Version: 4.6.0
Vcs-Browser: https://salsa.debian.org/debian/util-linux
Vcs-Git: https://salsa.debian.org/debian/util-linux.git
Testsuite: autopkgtest
Testsuite-Triggers: bash, bc, build-essential, dpkg, grep, pkg-config
Build-Depends: asciidoctor, bc <!stage1 !nocheck>, bison, debhelper-compat (= 13), dh-exec, gettext, libaudit-dev [linux-any] <!stage1>, libcap-ng-dev [linux-any] <!stage1>, libcryptsetup-dev [linux-any] <!pkg.util-linux.noverity>, libncurses5-dev, libncursesw5-dev, libpam0g-dev <!stage1>, libreadline-dev, libselinux1-dev [linux-any], libsystemd-dev [linux-any] <!stage1>, libtool, libudev-dev [linux-any] <!stage1>, netbase <!stage1 !nocheck>, pkg-config, po-debconf, socat <!stage1 !nocheck>, systemd [linux-any] <!stage1>, zlib1g-dev
Build-Conflicts: libedit-dev
Package-List:
 bsdextrautils deb utils optional arch=any profile=!stage1
 bsdutils deb utils required arch=any profile=!stage1 essential=yes
 eject deb utils optional arch=linux-any
 eject-udeb udeb debian-installer optional arch=linux-any profile=!noudeb
 fdisk deb utils important arch=any profile=!stage1
 fdisk-udeb udeb debian-installer optional arch=hurd-any,linux-any profile=!stage1,!noudeb
 libblkid-dev deb libdevel optional arch=any
 libblkid1 deb libs optional arch=any
 libblkid1-udeb udeb debian-installer optional arch=any profile=!noudeb
 libfdisk-dev deb libdevel optional arch=any
 libfdisk1 deb libs optional arch=any
 libfdisk1-udeb udeb debian-installer optional arch=any profile=!noudeb
 libmount-dev deb libdevel optional arch=linux-any
 libmount1 deb libs optional arch=any
 libmount1-udeb udeb debian-installer optional arch=linux-any profile=!noudeb
 libsmartcols-dev deb libdevel optional arch=any
 libsmartcols1 deb libs optional arch=any
 libsmartcols1-udeb udeb debian-installer optional arch=any profile=!noudeb
 libuuid1 deb libs optional arch=any
 libuuid1-udeb udeb debian-installer optional arch=any profile=!noudeb
 mount deb admin required arch=linux-any profile=!stage1
 rfkill deb utils optional arch=linux-any profile=!stage1
 util-linux deb utils required arch=any profile=!stage1 essential=yes
 util-linux-extra deb utils standard arch=any profile=!stage1
 util-linux-locales deb localization optional arch=all profile=!stage1
 util-linux-udeb udeb debian-installer optional arch=any profile=!stage1,!noudeb
 uuid-dev deb libdevel optional arch=any
 uuid-runtime deb utils optional arch=any profile=!stage1
Checksums-Sha1:
 f62a7b6fe64ce7f4569b57d7d2d0875b39f79836 7495904 util-linux_2.38.1.orig.tar.xz
 58df41d3ce418dfb6dd7b9539d141e8469377d8b 95928 util-linux_2.38.1-1.debian.tar.xz
Checksums-Sha256:
 60492a19b44e6cf9a3ddff68325b333b8b52b6c59ce3ebd6a0ecaa4c5117e84f 7495904 util-linux_2.38.1.orig.tar.xz
 c7a87ca093e565fbaed813b7e8b220f5e875437dd418ded2aefef6322f8aa0b8 95928 util-linux_2.38.1-1.debian.tar.xz
Files:
 cd11456f4ddd31f7fbfdd9488c0c0d02 7495904 util-linux_2.38.1.orig.tar.xz
 dca33ad63f75f3a844a17ced94fa4388 95928 util-linux_2.38.1-1.debian.tar.xz

-----BEGIN PGP SIGNATURE-----

iQIzBAEBCAAdFiEEfRrP+tnggGycTNOSXBPW25MFLgMFAmLr9qEACgkQXBPW25MF
LgOOCA/+JNjE88q2JBZXP+IqWCnGHfi2R34H/j6+153TKNLpm954nVrdzj8mH5Mn
X9lsKo27d3Hh/FSMlqe1SIAQfn/RpXWGUDO/nMYs6Rl/ZcLbq5B5uynjl0RW+tHm
hD61FnmbaYv2RUekEDeZStW69yE966mupTYeKqbDhow5nLzM+LMsUETedoB1smMZ
M3XGmRWtANszLbXSzWHDp/AVCdkrb6SkdRgXLZJcXf8WJzlVpRD/z786vdNvTK/J
Yboqd6refh9DGyuat0e+Y3NT+cpHyOhCpsZysibCXGs5ajeor5S0yuVwG33ZMyUO
wGoHBhnoi94BBtfcD34s/dDGARh3QVRtBG8FcDyHQ9wPZq9uNFap+zTYxQM9wBoB
eSroyaRIRZR0cREZpuwMqjJc50dAjQRUI+4RkTIUNuH3hXr7jgpjg01eYfKbYMF0
YRT/5qzCDMBZvNiu5XpALEKkw/Dw30e6Nbfs5kUV+QU88juXuc2BR+S351hd4uLJ
nUUZLLA4cT+h8IAsbMeFk6rm3tNyafHwfUOIz0Gr2h1D2+aD1F4h9DU2Y42FtnaY
SuwXODqAJHUVV2/RWDz3mO6JzEy0xhvu1Ak8+yy0Ol6c5aXH+S9Yr5NnVgtKgAGC
pz5Y3cNuy4BW/44a594CK2R2iCkz3o1V2SmmJxmIN1vMO/yWADY=
=kd5R
-----END PGP SIGNATURE-----
