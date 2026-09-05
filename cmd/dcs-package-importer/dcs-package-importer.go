package main

import (
	"flag"
	"log"

	"github.com/Debian/dcs/internal/packageimporter"
)

func main() {
	var opts packageimporter.Opts

	flag.StringVar(&opts.ListenAddress, "listen_address",
		":21010",
		"listen address ([host]:port)")

	flag.StringVar(&opts.SourceBackendAddr, "source_backend",
		"localhost:28081",
		"source backend host:port address")

	flag.StringVar(&opts.ShardPath, "shard_path",
		"/srv/dcs/shard0",
		"Path to the shard directory (containing src, idx, full)")

	flag.StringVar(&opts.CPUProfile, "cpuprofile",
		"",
		"write cpu profile to this file")

	flag.BoolVar(&opts.DebugSkip, "debug_skip",
		false,
		"Print log messages when files are skipped")

	flag.StringVar(&opts.TLSCertPath, "tls_cert_path", "", "Path to a .pem file containing the TLS certificate.")

	flag.StringVar(&opts.TLSKeyPath, "tls_key_path", "", "Path to a .pem file containing the TLS private key.")

	flag.Parse()

	if err := opts.Main(); err != nil {
		log.Fatal(err)
	}
}
