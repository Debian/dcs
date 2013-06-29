#!/usr/bin/env perl
# vim:ts=4:sw=4:expandtab

use strict;
use warnings;
use File::Basename;

sub link_contents {
    my ($src, $dst) = @_;

    my @contents = <$src/*>;
    # Safety-Check: We are already _in_ a Go library. Don’t copy its
    # subfolders, this has no use and potentially only screws things up.
    # This situation should never happen, unless some package ships files that
    # are already shipped in another package.
    my @gosrc = grep { /\.go$/ } @contents;
    return if @gosrc > 0;
    my @dirs = grep { -d } @contents;
    for my $dir (@dirs) {
        my $base = basename($dir);
        if (-d "$dst/$base") {
            link_contents("$src/$base", "$dst/$base");
        } else {
            symlink("$src/$base", "$dst/$base");
        }
    }
}

my $src = shift;
my $dst = shift;
link_contents($src, $dst);
