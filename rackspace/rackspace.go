// Debian Code Search-specific Rackspace API. This is not a generic Rackspace
// API. If you feel like it, fork it, improve it and maintain it (!). I don’t
// want to maintain it, so I don’t publish it as a generic package.
package rackspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"
)

var (
	rackspaceUser = flag.String("rackspace_user",
		"stapelberg",
		"Rackspace API credentials: username")
	rackspaceApiKey = flag.String("rackspace_api_key",
		"",
		"Rackspace API credentials: API key")
)

type RackspaceClient struct {
	token    tokenResponse
	TenantId string
}

func NewClient() (*RackspaceClient, error) {
	rs := &RackspaceClient{}

	if *rackspaceApiKey == "" {
		log.Fatal("Cannot authenticate: specify non-empty -rackspace_api_key")
	}

	// XXX: authenticate() currently does not extract the endpoints from the
	// response, but I’m not sure whether that is necessary/useful.
	token, err := rs.authenticate(*rackspaceUser, *rackspaceApiKey)
	if err != nil {
		return nil, err
	}

	rs.token = token
	// For easier access in the code
	rs.TenantId = token.Tenant.Id
	return rs, nil
}

type tenantResponse struct {
	Id string `json:"id"`
}

type tokenResponse struct {
	Id     string         `json:"id"`
	Tenant tenantResponse `json:"tenant"`
}

func (rs *RackspaceClient) request(method string, url string, input io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, input)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// request() is also used in authenticate(), i.e. bofer rs.token was ever set.
	if rs.token.Id != "" {
		req.Header.Set("X-Auth-Token", rs.token.Id)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	// POST /servers returns the “202 Accepted” StatusCode, so we treat that
	// as successful in any response.
	// DELETE /servers/<id> returns “204 No Content”, so that’s okay, too.
	if resp.StatusCode != 200 &&
		resp.StatusCode != 202 &&
		resp.StatusCode != 204 {
		log.Printf("DEBUG: response = %v\n", resp)
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		err = errors.New(string(body))
		return nil, err
	}

	return resp, nil
}

func (rs *RackspaceClient) authenticate(user, apikey string) (token tokenResponse, err error) {
	type credentials struct {
		Username string `json:"username"`
		Apikey   string `json:"apiKey"`
	}
	type innerAuthRequest struct {
		Credentials credentials `json:"RAX-KSKEY:apiKeyCredentials"`
	}
	type authRequest struct {
		Inner innerAuthRequest `json:"auth"`
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.Encode(&authRequest{
		Inner: innerAuthRequest{
			Credentials: credentials{
				Username: user,
				Apikey:   apikey,
			},
		},
	})

	resp, err := rs.request("POST", "https://identity.api.rackspacecloud.com/v2.0/tokens", &buf)
	if err != nil {
		return
	}

	type accessResponse struct {
		Token tokenResponse `json:"token"`
	}

	type authResponse struct {
		Access accessResponse `json:"access"`
	}

	var decoded authResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return token, err
	}

	return decoded.Access.Token, nil
}

func (rs *RackspaceClient) GetDomainId(domain string) (int, error) {
	url := fmt.Sprintf("https://dns.api.rackspacecloud.com/v1.0/%s/domains?name=%s", rs.TenantId, domain)
	resp, err := rs.request("GET", url, nil)
	if err != nil {
		return 0, err
	}

	type domainResponse struct {
		Name string `json:"name"`
		Id   int    `json:"id"`
	}

	type getDomainResponse struct {
		Domains []domainResponse `json:"domains"`
	}

	var decoded getDomainResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return 0, err
	}

	for _, d := range decoded.Domains {
		if d.Name == domain {
			return d.Id, nil
		}
	}

	return 0, errors.New("No such domain")
}

func (rs *RackspaceClient) GetDomainRecords(domainId int) ([]Record, error) {
	url := fmt.Sprintf("https://dns.api.rackspacecloud.com/v1.0/%s/domains/%d/records", rs.TenantId, domainId)
	resp, err := rs.request("GET", url, nil)
	if err != nil {
		return []Record{}, err
	}

	type getRecordsResponse struct {
		Records []Record `json:"records"`
	}
	var decoded getRecordsResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return []Record{}, err
	}

	return decoded.Records, nil
}

func (rs *RackspaceClient) UpdateRecords(domainId int, records []Record) error {
	type recordsRequest struct {
		Records []Record `json:"records"`
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.Encode(&recordsRequest{Records: records})

	url := fmt.Sprintf("https://dns.api.rackspacecloud.com/v1.0/%s/domains/%d/records", rs.TenantId, domainId)
	_, err := rs.request("PUT", url, &buf)
	return err
}

type Record struct {
	Name string `json:"name"`
	Id   string `json:"id"`
	Type string `json:"type,omitempty"`
	Data string `json:"data,omitempty"`
	Ttl  int    `json:"ttl,omitempty"`
}

type Image struct {
	Name string `json:"name"`
	Id   string `json:"id"`
}

func (rs *RackspaceClient) getSnapshots() ([]Image, error) {
	url := fmt.Sprintf("https://ord.servers.api.rackspacecloud.com/v2/%s/images?type=snapshot", rs.TenantId)
	resp, err := rs.request("GET", url, nil)
	if err != nil {
		return []Image{}, err
	}

	type getImagesResponse struct {
		Images []Image `json:"images"`
	}

	var decoded getImagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return []Image{}, err
	}

	return decoded.Images, nil
}

func (rs *RackspaceClient) getSnapshotId(prefix string) (string, error) {
	snapshots, err := rs.getSnapshots()
	if err != nil {
		return "", err
	}

	for _, snapshot := range snapshots {
		if strings.HasPrefix(snapshot.Name, prefix) {
			return snapshot.Id, nil
		}
	}

	return "", fmt.Errorf(`No snapshot has prefix "%s"`, prefix)
}

type Address struct {
	Addr    string `json:"addr"`
	Version int    `json:"version"`
}

type Server struct {
	client *RackspaceClient

	Name string `json:"name"`
	Id   string `json:"id"`
	// "ACTIVE" ideally, or "BUILD" after being created.
	RawStatus  string `json:"status"`
	RawUpdated string `json:"updated"`
	RawCreated string `json:"created"`

	// Percentage value during Status == "BUILD"
	Progress int `json:"progress"`

	AccessIPv4 string `json:"accessIPv4"`
	AccessIPv6 string `json:"accessIPv6"`

	Addresses map[string][]Address `json:"addresses"`
}

func (s Server) Updated() time.Time {
	updated, err := time.Parse(time.RFC3339, s.RawUpdated)
	if err != nil {
		log.Fatal("Rackspace API is broken: 'updated' not parseable: %v\n", err)
	}

	return updated
}

func (s Server) Created() time.Time {
	created, err := time.Parse(time.RFC3339, s.RawCreated)
	if err != nil {
		log.Fatal("Rackspace API is broken: 'created' not parseable: %v\n", err)
	}

	return created
}

func (s Server) Status() string {
	return strings.ToUpper(s.RawStatus)
}

func (s Server) PrivateIPv4() string {
	for _, addr := range s.Addresses["private"] {
		if addr.Version == 4 {
			return addr.Addr
		}
	}

	log.Fatal("Rackspace API is broken: server without a private IPv4 address: %v\n", s)
	return ""
}

// TODO: make sure that no round-trip is done when s.Status() is already acceptible
func (s Server) BecomeActiveOrDie(timeout time.Duration) Server {
	// Wait for the server to become ACTIVE.
	thing := statusTransitionOrDie(
		func() (StatusProvider, error) { return s.client.GetServer(s.Id) },
		[]string{"ACTIVE"},
		timeout)
	newServer, _ := thing.(Server)
	return newServer
}

// Block Storage Volume
type Volume struct {
	client      *RackspaceClient
	DisplayName string `json:"display_name"`
	Id          string `json:"id"`
	RawStatus   string `json:"status"`
}

func (v Volume) Status() string {
	return strings.ToUpper(v.RawStatus)
}

func (v Volume) becomeStatusOrDie(statuses []string, timeout time.Duration) Volume {
	thing := statusTransitionOrDie(
		func() (StatusProvider, error) { return v.client.GetVolume(v.Id) },
		statuses,
		timeout)
	volume, _ := thing.(Volume)
	log.Printf("Volume became active, details = %v\n", volume)
	return volume
}

func (v Volume) BecomeAvailableOrDie(timeout time.Duration) Volume {
	return v.becomeStatusOrDie([]string{"AVAILABLE", "IN-USE"}, timeout)
}

func (v Volume) BecomeInUseOrDie(timeout time.Duration) Volume {
	return v.becomeStatusOrDie([]string{"IN-USE"}, timeout)
}

func (rs *RackspaceClient) GetServers() ([]Server, error) {
	url := fmt.Sprintf("https://ord.servers.api.rackspacecloud.com/v2/%s/servers/detail", rs.TenantId)
	resp, err := rs.request("GET", url, nil)
	if err != nil {
		return []Server{}, err
	}

	type getServersResponse struct {
		Servers []Server `json:"servers"`
	}

	var decoded getServersResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return []Server{}, err
	}

	return decoded.Servers, nil
}

func (rs *RackspaceClient) GetServer(serverId string) (Server, error) {
	url := fmt.Sprintf("https://ord.servers.api.rackspacecloud.com/v2/%s/servers/%s", rs.TenantId, serverId)
	resp, err := rs.request("GET", url, nil)
	if err != nil {
		return Server{}, err
	}

	type getServerResponse struct {
		Server Server `json:"server"`
	}

	var decoded getServerResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return Server{}, err
	}

	decoded.Server.client = rs
	return decoded.Server, nil
}

func (rs *RackspaceClient) DeleteServer(serverId string) error {
	url := fmt.Sprintf("https://ord.servers.api.rackspacecloud.com/v2/%s/servers/%s", rs.TenantId, serverId)
	_, err := rs.request("DELETE", url, nil)
	return err
}

func (rs *RackspaceClient) createServer(request CreateServerRequest) (string, error) {
	type serverRequest struct {
		Server CreateServerRequest `json:"server"`
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.Encode(&serverRequest{Server: request})

	url := fmt.Sprintf("https://ord.servers.api.rackspacecloud.com/v2/%s/servers", rs.TenantId)
	resp, err := rs.request("POST", url, &buf)
	if err != nil {
		return "", err
	}

	type createServerResponse struct {
		Server Server `json:"server"`
	}

	var decoded createServerResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", err
	}

	return decoded.Server.Id, nil
}

func (rs *RackspaceClient) CreateBlockStorage(request CreateBlockStorageRequest) (string, error) {
	type blockStorageRequest struct {
		Volume CreateBlockStorageRequest `json:"volume"`
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.Encode(&blockStorageRequest{Volume: request})

	url := fmt.Sprintf("https://ord.blockstorage.api.rackspacecloud.com/v1/%s/volumes", rs.TenantId)
	resp, err := rs.request("POST", url, &buf)
	if err != nil {
		return "", err
	}

	type createBlockStorageResponse struct {
		Volume Volume `json:"volume"`
	}

	var decoded createBlockStorageResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", err
	}

	return decoded.Volume.Id, nil
}

func (rs *RackspaceClient) AttachBlockStorage(serverId string, request VolumeAttachmentRequest) (string, error) {
	type attachBlockStorageRequest struct {
		VolumeAttachment VolumeAttachmentRequest `json:"volumeAttachment"`
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.Encode(&attachBlockStorageRequest{VolumeAttachment: request})

	url := fmt.Sprintf("https://ord.servers.api.rackspacecloud.com/v2/%s/servers/%s/os-volume_attachments", rs.TenantId, serverId)
	resp, err := rs.request("POST", url, &buf)
	if err != nil {
		return "", err
	}

	type volumeAttachment struct {
		Id string `json:"id"`
	}

	type attachBlockStorageResponse struct {
		VolumeAttachment volumeAttachment `json:"volumeAttachment"`
	}

	var decoded attachBlockStorageResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", err
	}

	return decoded.VolumeAttachment.Id, nil

}

func (rs *RackspaceClient) GetVolumes() ([]Volume, error) {
	url := fmt.Sprintf("https://ord.blockstorage.api.rackspacecloud.com/v1/%s/volumes", rs.TenantId)
	resp, err := rs.request("GET", url, nil)
	if err != nil {
		return []Volume{}, err
	}

	type getVolumesResponse struct {
		Volumes []Volume `json:"volumes"`
	}

	var decoded getVolumesResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return []Volume{}, err
	}

	return decoded.Volumes, nil

}

func (rs *RackspaceClient) GetVolume(volumeId string) (Volume, error) {
	url := fmt.Sprintf("https://ord.blockstorage.api.rackspacecloud.com/v1/%s/volumes/%s", rs.TenantId, volumeId)
	resp, err := rs.request("GET", url, nil)
	if err != nil {
		return Volume{}, err
	}

	type getVolumeResponse struct {
		Volume Volume `json:"volume"`
	}

	var decoded getVolumeResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return Volume{}, err
	}

	decoded.Volume.client = rs
	return decoded.Volume, nil
}

func (rs *RackspaceClient) DeleteVolume(volumeId string) error {
	url := fmt.Sprintf("https://ord.blockstorage.api.rackspacecloud.com/v1/%s/volumes/%s", rs.TenantId, volumeId)
	_, err := rs.request("DELETE", url, nil)
	return err
}

type KeypairRequest struct {
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
}

func (rs *RackspaceClient) createKeypair(name string, pubkey string) error {
	type uploadKeypairRequest struct {
		Keypair KeypairRequest `json:"keypair"`
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.Encode(&uploadKeypairRequest{
		Keypair: KeypairRequest{Name: name, PublicKey: pubkey}})

	url := fmt.Sprintf("https://ord.servers.api.rackspacecloud.com/v2/%s/os-keypairs", rs.TenantId)
	resp, err := rs.request("POST", url, &buf)
	if err != nil {
		return err
	}
	log.Printf("resp = %v\n", resp)
	body, _ := ioutil.ReadAll(resp.Body)
	log.Printf("body = %v\n", string(body))
	return nil
}

type CreateServerRequest struct {
	Name      string `json:"name"`
	ImageRef  string `json:"imageRef"`
	FlavorRef string `json:"flavorRef"`
	// Either AUTO or MANUAL.
	DiskConfig string `json:"OS-DCF:diskConfig"`
	KeyName    string `json:"key_name"`
}

type CreateBlockStorageRequest struct {
	DisplayName string `json:"display_name"`
	Size        int    `json:"size"`
	VolumeType  string `json:"volume_type"`
}

type VolumeAttachmentRequest struct {
	Device   string `json:"device"`
	VolumeId string `json:"volumeId"`
}

func (rs *RackspaceClient) ServerFromSnapshot(snapshot string, request CreateServerRequest) string {
	// Find the correct snapshot ID.
	snapshotId, err := rs.getSnapshotId(snapshot)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("snapshot id = %v\n", snapshotId)

	// Make sure the keypair exists.
	pubkey, err := ioutil.ReadFile("/home/michael/.ssh/dcs-auto-rs.pub")
	if err != nil {
		log.Fatalf("Cannot read SSH public key: %v\n", err)
	}

	err = rs.createKeypair("dcs-auto-rs", string(pubkey))
	if err != nil {
		log.Printf("error creating keypair (ignoring): %v\n", err)
	}

	// Create the server on which the index will be created (unless a server ID
	// was already specified).
	request.ImageRef = snapshotId
	request.KeyName = "dcs-auto-rs"
	// The base image uses MANUAL, but let’s be sure and specify it
	// here, too. MANUAL servers come up faster.
	request.DiskConfig = "MANUAL"
	serverId, err := rs.createServer(request)

	if err != nil {
		log.Fatal(err)
	}

	return serverId
}

type StatusProvider interface {
	Status() string
}

// TODO: simplify this prototype (typedef, shorter names)
func statusTransitionOrDie(returnFunc func() (StatusProvider, error), acceptableStatuses []string, timeout time.Duration) StatusProvider {
	var ret StatusProvider
	var err error

	// For conveniently determining whether a status is acceptable later on.
	acceptable := make(map[string]bool)
	for _, status := range acceptableStatuses {
		acceptable[status] = true
	}

	var status string
	start := time.Now()
	for time.Since(start) < timeout {
		ret, err = returnFunc()
		if err != nil {
			log.Fatal(err)
		}

		status = ret.Status()
		if acceptable[status] {
			log.Printf("Thing became %s.\n", status)
			break
		}

		if status == "ERROR" {
			log.Fatal("Thing has status ERROR, aborting.\n")
		}

		server, ok := ret.(Server)
		if ok {
			log.Printf(`Server status "%s", progress %d/100`, status, server.Progress)
		} else {
			log.Printf(`Status "%s"`, status)
		}
		time.Sleep(10 * time.Second)
	}

	if !acceptable[status] {
		log.Fatalf("Expected server to become one of %v within %d minutes, instead it is %s\n",
			acceptableStatuses, timeout.Minutes(), status)
	}

	return ret
}
