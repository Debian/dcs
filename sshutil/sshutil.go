// Convenience wrapper around go.crypto/ssh
package sshutil

import (
	"code.google.com/p/go.crypto/ssh"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"log"
)

type keychain struct {
	key *rsa.PrivateKey
}

func (k *keychain) Key(i int) (ssh.PublicKey, error) {
	if i != 0 {
		return nil, nil
	}
	return ssh.NewPublicKey(&k.key.PublicKey)
}

func (k *keychain) Sign(i int, rand io.Reader, data []byte) (sig []byte, err error) {
	hashFunc := crypto.SHA1
	h := hashFunc.New()
	h.Write(data)
	digest := h.Sum(nil)
	return rsa.SignPKCS1v15(rand, k.key, hashFunc, digest)
}

type Client struct {
	*ssh.ClientConn

	Host string
}

func Connect(host string) (*Client, error) {
	// TODO: flag for the key file
	clientauth, err := OpenSshClientAuth("/home/michael/.ssh/dcs-auto-rs")
	if err != nil {
		log.Fatal(err)
	}

	clientConfig := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.ClientAuth{clientauth},
	}

	clientconn, err := ssh.Dial("tcp", "["+host+"]:22", clientConfig)
	if err != nil {
		return nil, err
	}
	return &Client{clientconn, host}, nil
}

// Reads an OpenSSH key and provides it as a ssh.ClientAuth.
func OpenSshClientAuth(path string) (ssh.ClientAuth, error) {
	privateKey, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(privateKey)
	if block == nil {
		return nil, fmt.Errorf(`No key data found in PEM file "%s"`, path)
	}

	rsakey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	clientKey := &keychain{rsakey}
	return ssh.ClientAuthKeyring(clientKey), nil
}

// Wrapper around CombinedOutput() that dies when the command fails.
func (c *Client) RunOrDie(command string) string {
	session, err := c.NewSession()
	if err != nil {
		log.Fatalf("Failed to create session in SSH connection: %v\n", err)
	}
	defer session.Close()
	log.Printf(`[SSH %s] Running "%s"`, c.Host, command)
	output, err := session.CombinedOutput(command)
	if err != nil {
		log.Fatalf(`Could not execute SSH command "%s":
%s
%v`, command, output, err)
	}
	return string(output)
}

func (c *Client) Successful(command string) bool {
	session, err := c.NewSession()
	if err != nil {
		log.Fatalf("Failed to create session in SSH connection: %v\n", err)
	}
	defer session.Close()
	return session.Run(command) == nil
}

func (c *Client) WriteToFileOrDie(filename string, content []byte) {
	session, err := c.NewSession()
	if err != nil {
		log.Fatalf("Failed to create session in SSH connection: %v\n", err)
	}
	defer session.Close()

	pipe, err := session.StdinPipe()
	if err != nil {
		log.Fatalf("Failed to create stdin pipe in SSH connection: %v\n", err)
	}

	go func() {
		pipe.Write(content)
		pipe.Close()
	}()

	err = session.Run("cat > " + filename)
	if err != nil {
		log.Fatalf(`Failed to write "%s": %v`, filename, err)
	}
}
