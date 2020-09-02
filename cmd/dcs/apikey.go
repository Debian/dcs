package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Debian/dcs/internal/apikeys"
	"github.com/gorilla/securecookie"
)

const apikeyCreateHelp = `apikey-create - create a new API key


Example:
  % dcs apikey-create -subject "manually-created-by-stapelberg!janitor"
`

func apikeyCreate(args []string) error {
	fset := flag.NewFlagSet("apikey-create", flag.ExitOnError)
	fset.Usage = usage(fset, apikeyCreateHelp)

	var subject string
	fset.StringVar(&subject, "subject", "", "subject")

	hashKeyStr := fset.String("securecookie_hash_key",
		"",
		"32-byte hexadecimal key for HMAC-based secure cookie storage (hashing, i.e. for authentication)")

	blockKeyStr := fset.String("securecookie_block_key",
		"",
		"32-byte hexadecimal key for HMAC-based secure cookie storage (block, i.e. for encryption)")

	if err := fset.Parse(args); err != nil {
		return err
	}
	if subject == "" {
		fset.Usage()
		os.Exit(1)
	}

	if *hashKeyStr == "" {
		return fmt.Errorf("-securecookie_hash_key is required. E.g.: -securecookie_hash_key=%x", securecookie.GenerateRandomKey(32))
	}

	hashKey, err := hex.DecodeString(*hashKeyStr)
	if err != nil {
		return err
	}

	if *blockKeyStr == "" {
		return fmt.Errorf("-securecookie_block_key is required. E.g.: -securecookie_block_key=%x", securecookie.GenerateRandomKey(32))
	}

	blockKey, err := hex.DecodeString(*blockKeyStr)
	if err != nil {
		return err
	}

	opts := apikeys.Options{
		HashKey:  hashKey,
		BlockKey: blockKey,
	}
	cookies := opts.SecureCookie()

	key := apikeys.Key{
		Subject:              subject,
		CreatedUnixTimestamp: time.Now().Unix(),
	}
	encoded, err := cookies.Encode("token", key)
	if err != nil {
		return err
	}

	fmt.Println(encoded)

	return nil
}

const apikeyVerifyHelp = `apikey-verify - decode and verify an API key


Example:
  % dcs apikey-verify <key>
`

func apikeyVerify(args []string) error {
	fset := flag.NewFlagSet("apikey-verify", flag.ExitOnError)
	fset.Usage = usage(fset, apikeyVerifyHelp)

	hashKeyStr := fset.String("securecookie_hash_key",
		"",
		"32-byte hexadecimal key for HMAC-based secure cookie storage (hashing, i.e. for authentication)")

	blockKeyStr := fset.String("securecookie_block_key",
		"",
		"32-byte hexadecimal key for HMAC-based secure cookie storage (block, i.e. for encryption)")

	if err := fset.Parse(args); err != nil {
		return err
	}
	if fset.NArg() == 0 {
		fset.Usage()
		os.Exit(1)
	}

	if *hashKeyStr == "" {
		return fmt.Errorf("-securecookie_hash_key is required. E.g.: -securecookie_hash_key=%x", securecookie.GenerateRandomKey(32))
	}

	hashKey, err := hex.DecodeString(*hashKeyStr)
	if err != nil {
		return err
	}

	if *blockKeyStr == "" {
		return fmt.Errorf("-securecookie_block_key is required. E.g.: -securecookie_block_key=%x", securecookie.GenerateRandomKey(32))
	}

	blockKey, err := hex.DecodeString(*blockKeyStr)
	if err != nil {
		return err
	}

	opts := apikeys.Options{
		HashKey:  hashKey,
		BlockKey: blockKey,
	}
	dec := apikeys.Decoder{
		SecureCookie: opts.SecureCookie(),
	}
	for _, apikey := range fset.Args() {
		key, err := dec.Decode(apikey)
		if err != nil {
			return err
		}
		log.Printf("API key %q successfully verified:", apikey)
		log.Printf("  subject: %s", key.Subject)
		log.Printf("  created: %v", time.Unix(key.CreatedUnixTimestamp, 0))
	}

	return nil
}
