package main

import (
	"flag"
	"log"

	"github.com/Debian/dcs/internal/web"
)

func main() {
	var opts web.Opts

	flag.StringVar(&opts.ListenAddressPlain, "listen_address_http",
		"",
		"listen address ([host]:port)")

	flag.StringVar(&opts.ListenAddress, "listen_address",
		"",
		"listen address ([host]:port) for gRPC/TLS")

	flag.StringVar(&opts.MemProfile, "memprofile", "", "Write memory profile to this file")

	flag.StringVar(&opts.StaticPath, "static_path",
		"./static/",
		"Path to static assets such as *.css")

	flag.StringVar(&opts.AccessLogPath, "access_log_path",
		"",
		"Where to write access.log entries (in Apache Common Log Format). Disabled if empty.")

	flag.StringVar(&opts.TLSCertPath, "tls_cert_path", "", "Path to a .pem file containing the TLS certificate.")

	flag.StringVar(&opts.TLSKeyPath, "tls_key_path", "", "Path to a .pem file containing the TLS private key.")

	flag.StringVar(&opts.HashKeyStr, "securecookie_hash_key",
		"",
		"32-byte hexadecimal key for HMAC-based secure cookie storage (hashing, i.e. for authentication)")

	flag.StringVar(&opts.BlockKeyStr, "securecookie_block_key",
		"",
		"32-byte hexadecimal key for HMAC-based secure cookie storage (block, i.e. for encryption)")

	flag.StringVar(&opts.ClickLogPath, "click_log_path",
		"",
		"Where to write the click.log entries (JSON-encoded, timestamped). Disabled if empty.")

	flag.StringVar(&opts.ClientID, "salsa_application_id",
		// Application ID for “Dev Test Debian Code Search API”,
		// with only 127.0.0.1 as Callback URL
		"5f03a84a9ade19cd12e2666a3da8a5d00af9d3d8b0bcde4d96d48d50064b4d6d",
		"salsa.debian.org GitLab Application ID")

	flag.StringVar(&opts.ClientSecret, "salsa_application_secret",
		// Okay to leak; Callback URL limited to local development.
		"3e1c36935af8570f3c916200956c129525954f4f19c06cd0abf275db2faca192",
		"salsa.debian.org GitLab Application Secret")

	flag.StringVar(&opts.RedirectURL, "salsa_application_callback_url",
		"https://127.0.0.1:28080/apikeys/redirect_uri",
		"salsa.debian.org GitLab Application login flow callback URL (fully qualified)")

	flag.BoolVar(&opts.PrintVersion, "version",
		false,
		"print version and exit")

	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if err := opts.Main(); err != nil {
		log.Fatal(err)
	}
}
