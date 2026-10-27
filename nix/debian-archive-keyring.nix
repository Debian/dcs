{
  fetchurl,
  runCommand,
  gnupg,
  lib,
}:

let
  keys = [
    (fetchurl {
      # Debian 13 (trixie) archive automatic signing key.
      # 04B5 4C3C DCA7 9751 B16B C6B5 2256 29DF 75B1 88BD
      url = "https://ftp-master.debian.org/keys/archive-key-13.asc";
      hash = "sha256-bx0ndCndf/7cxvhoinrZpFiFmxE5/6Am0e6q3L/7Dac=";
    })
  ];
in
runCommand "debian-archive-keyring.gpg" { nativeBuildInputs = [ gnupg ]; } ''
  cat ${lib.concatStringsSep " " keys} | gpg --dearmor > $out
''
