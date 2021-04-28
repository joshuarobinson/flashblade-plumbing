package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"
)

// supportedRestVersions is used to negotiate the API version to use
var supportedRestVersions = [...]string{"1.0", "1.1", "1.2", "1.3", "1.4", "1.5", "1.6", "1.7", "1.8", "1.9", "1.10", "1.11"}

type supported struct {
	Versions []string `json:"versions"`
}

type PaginationInfo struct {
	TotalItemCount    int    `json:"total_item_count"`
	ContinuationToken string `json:"continuation_token"`
}

type FixedReferenceWithId struct {
	Id           string `json:"id"`
	Name         string `json:"name"`
	ResourceType string `json:"resource_type"`
}

type NetworkInterface struct {
	Id       string               `json:"id"`
	Name     string               `json:"name"`
	Address  string               `json:"address"`
	Enabled  bool                 `json:"enabled"`
	Gateway  string               `json:"gateway"`
	MTU      int                  `json:"mtu"`
	Netmask  string               `json:"netmask"`
	Services []string             `json:"services"`
	Subnet   FixedReferenceWithId `json:"subnet"`
	Type     string               `json:"type"`
	Vlan     int                  `json:"vlan"`
}

type NetworkInterfaceResponse struct {
	paginationInfo PaginationInfo     `json:"pagination_info"`
	Items          []NetworkInterface `json:"items"`
}

type NfsRule struct {
	Enabled    bool   `json:"enabled"`
	Rules      string `json:"rules,omitempty"`
	V3Enabled  bool   `json:"v3_enabled"`
	V41Enabled bool   `json:"v4_1_enabled"`
}

type Reference struct {
	Name         string `json:"name"`
	Id           string `json:"id"`
	ResourceType string `json:"resource_type"`
}

type LocationReference struct {
	Name         string    `json:"name"`
	Id           string    `json:"id"`
	ResourceType string    `json:"resource_type"`
	Location     Reference `json:"location"`
	DisplayName  string    `json:"display_name"`
	IsLocal      bool      `json:"is_local"`
}

type Space struct {
	Virtual       int     `json:"virtual"`
	DataReduction float64 `json:"data_reduction"`
	Unique        int     `json:"unique"`
	Snapshots     int     `json:"snapshots"`
	TotalPhysical int     `json:"total_physical"`
}

type FileSystem struct {
	Name                       string  `json:"name,omitempty"`
	Created                    int     `json:"created,omitempty"`
	Id                         string  `json:"id,omitempty"`
	DefaultUserQuota           int     `json:"default_user_quota,omitempty"`
	DefaultGroupQuota          int     `json:"default_group_quota,omitempty"`
	Destroyed                  bool    `json:"destroyed,omitempty"`
	FastRemoveDirectoryEnabled bool    `json:"fast_remove_directory_enabled,omitempty"`
	Nfs                        NfsRule `json:"nfs,omitempty"`
	Provisioned                int     `json:"provisioned,omitempty"`
	SnapshotDirectoryEnabled   bool    `json:"snapshot_directory_enabled,omitempty"`
}

type UserType struct {
	Name string `json:"name,omitempty"`
	Id   string `json:"id,omitempty"`
}

type ObjectStoreAccessKeyPost struct {
	User UserType `json:"user"`
}

type ObjectStoreAccessKey struct {
	Name            string   `json:"name"`
	Created         int      `json:"created"`
	User            UserType `json:"user"`
	Enabled         bool     `json:"enabled"`
	SecretAccessKey string   `json:"secret_access_key"`
}

type ObjectStoreAccessKeyResponse struct {
	paginationInfo PaginationInfo         `json:"pagination_info"`
	Items          []ObjectStoreAccessKey `json:"items"`
}

type BucketPost struct {
	Account UserType `json:"account"`
}

type BucketPatch struct {
	Destroyed  bool   `json:"destroyed,omitempty"`
	Versioning string `json:"versioning,omitempty"`
}

type FlashBladeClient struct {
	Target      string
	APIToken    string
	client      *http.Client
	RestVersion string

	xauthToken string
}

func getAPIVersion(uri string) (string, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	var c = &http.Client{Timeout: 10 * time.Second, Transport: tr}
	r, err := c.Get(uri)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()

	bodyBytes, _ := ioutil.ReadAll(r.Body)
	var target supported
	err = json.Unmarshal(bodyBytes, &target)
	if err != nil {
		return "", nil
	}

	for i := len(supportedRestVersions) - 1; i >= 0; i-- {
		for n := len(target.Versions) - 1; n >= 0; n-- {
			if supportedRestVersions[i] == target.Versions[n] {
				return target.Versions[n], nil
			}
		}
	}
	err = errors.New("[error] FlashBlade is incompatible with all supported REST API versions")
	return "", err
}

func (c *FlashBladeClient) formatPath(path string) string {
	return fmt.Sprintf("https://%s/api/%s/%s", c.Target, c.RestVersion, path)
}

func (c *FlashBladeClient) login() error {
	authURL, err := url.Parse("https://" + c.Target + "/api/login")
	req, err := http.NewRequest("POST", authURL.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Add("api-token", c.APIToken)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		c.xauthToken = resp.Header["X-Auth-Token"][0]
	} else {
		return fmt.Errorf("Login to FlashBalde at %s failed with status %d\n", c.Target, resp.StatusCode)
	}

	return nil
}

func (c *FlashBladeClient) logout() error {

	authURL, err := url.Parse("https://" + c.Target + "/api/logout")
	req, err := http.NewRequest("POST", authURL.String(), nil)
	if err != nil {
		return err
	}

	req.Header.Add("x-auth-token", c.xauthToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return err
}

func (c *FlashBladeClient) Close() {
	c.logout()
}

func (c *FlashBladeClient) SendRequest(method string, path string, params map[string]string, data []byte) (string, error) {

	if len(c.xauthToken) == 0 {
		err := errors.New("[error] Not currently logged in to FlashBlade, unable to send requests.")
		return "", err
	}

	baseURL, err := url.Parse(c.formatPath(path))
	if err != nil {
		return "", err
	}

	if params != nil {
		ps := url.Values{}
		for k, v := range params {
			ps.Set(k, v)
		}
		baseURL.RawQuery = ps.Encode()
	}

	req, err := http.NewRequest(method, baseURL.String(), bytes.NewBuffer(data))
	if err != nil {
		return "", err
	}
	req.Header.Add("content-type", "application/json; charset=utf-8")
	req.Header.Add("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Add("x-auth-token", c.xauthToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if c := resp.StatusCode; 200 < c || c > 299 {
		err := fmt.Errorf("[error %d] HTTP request %s %s did not succeed: ", resp.StatusCode, method, baseURL.String(), http.StatusText(c))
		return "", err
	}

	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	return string(bodyBytes), err
}

func (c *FlashBladeClient) ListNetworkInterfaces() ([]NetworkInterface, error) {
	respString, err := c.SendRequest("GET", "network-interfaces", nil, nil)
	if err != nil {
		return nil, err
	}

	var res NetworkInterfaceResponse
	err = json.Unmarshal([]byte(respString), &res)

	if res.paginationInfo.ContinuationToken != "" {
		fmt.Println("Not prepared for a continuation token in ListNetworkInterfaces")
	}
	return res.Items, err
}

func (c *FlashBladeClient) GetFileSystem(name string) (string, error) {

	var params = map[string]string{"names": name}

	respString, err := c.SendRequest("GET", "file-systems", params, nil)
	if err != nil {
		return "", err
	}
	return respString, err
}

func (c *FlashBladeClient) CreateFileSystem(filesystem FileSystem) error {

	data, err := json.Marshal(filesystem)
	if err != nil {
		return err
	}

	resp, err := c.SendRequest("POST", "file-systems", nil, data)
	if err != nil {
		fmt.Printf(resp)
		return err
	}
	return err
}

func (c *FlashBladeClient) DeleteFileSystem(name string) error {

	// Disable NFS
	var params = map[string]string{"name": name}
	var disable_nfs FileSystem
	disable_nfs.Nfs.Enabled = false
	data, err := json.Marshal(disable_nfs)
	if err != nil {
		return err
	}
	_, err = c.SendRequest("PATCH", "file-systems", params, data)
	if err != nil {
		return err
	}

	// Destroy
	var destroy_fs FileSystem
	destroy_fs.Destroyed = true
	data, err = json.Marshal(destroy_fs)
	if err != nil {
		return err
	}

	_, err = c.SendRequest("PATCH", "file-systems", params, data)
	if err != nil {
		return err
	}

	// Eradicate
	_, err = c.SendRequest("DELETE", "file-systems", params, nil)
	if err != nil {
		return err
	}

	return err
}

func (c *FlashBladeClient) CreateObjectStoreAccount(name string) error {

	var params = map[string]string{"names": name}
	_, err := c.SendRequest("POST", "object-store-accounts", params, nil)
	if err != nil {
		return err
	}
	return err
}

func (c *FlashBladeClient) DeleteObjectStoreAccount(name string) error {

	var params = map[string]string{"names": name}
	_, err := c.SendRequest("DELETE", "object-store-accounts", params, nil)
	if err != nil {
		return err
	}
	return err
}

func (c *FlashBladeClient) CreateObjectStoreUser(name string, account string) error {

	accountuser := account + "/" + name
	var params = map[string]string{"names": accountuser}
	_, err := c.SendRequest("POST", "object-store-users", params, nil)
	if err != nil {
		return err
	}
	return err
}

func (c *FlashBladeClient) DeleteObjectStoreUser(name string, account string) error {

	accountuser := account + "/" + name
	var params = map[string]string{"names": accountuser}
	_, err := c.SendRequest("DELETE", "object-store-users", params, nil)
	if err != nil {
		return err
	}
	return err
}

func (c *FlashBladeClient) CreateObjectStoreAccessKeys(name string, account string) ([]ObjectStoreAccessKey, error) {

	accountuser := account + "/" + name
	var post ObjectStoreAccessKeyPost
	post.User.Name = accountuser
	postdata, err := json.Marshal(post)
	if err != nil {
		return nil, err
	}

	respString, err := c.SendRequest("POST", "object-store-access-keys", nil, postdata)
	if err != nil {
		return nil, err
	}

	var res ObjectStoreAccessKeyResponse
	err = json.Unmarshal([]byte(respString), &res)

	if res.paginationInfo.ContinuationToken != "" {
		fmt.Println("Not prepared for a continuation token in CreateObjectStoreAccessKeys")
	}
	return res.Items, err
}

func (c *FlashBladeClient) DeleteObjectStoreAccessKey(name string) error {

	var params = map[string]string{"names": name}
	_, err := c.SendRequest("DELETE", "object-store-access-keys", params, nil)
	if err != nil {
		return err
	}
	return err
}

func (c *FlashBladeClient) CreateObjectStoreBucket(name string, account string) error {

	var params = map[string]string{"names": name}

	var post BucketPost
	post.Account.Name = account
	postdata, err := json.Marshal(post)
	if err != nil {
		return err
	}

	_, err = c.SendRequest("POST", "buckets", params, postdata)
	if err != nil {
		return err
	}
	return err
}

func (c *FlashBladeClient) DeleteObjectStoreBucket(name string) error {

	var params = map[string]string{"names": name}

	var patch BucketPatch
	patch.Destroyed = true
	patchdata, err := json.Marshal(patch)
	if err != nil {
		return err
	}

	_, err = c.SendRequest("PATCH", "buckets", params, patchdata)
	if err != nil {
		return err
	}

	_, err = c.SendRequest("DELETE", "buckets", params, nil)
	if err != nil {
		return err
	}
	return err
}

func NewFlashBladeClient(target string, apiToken string) (*FlashBladeClient, error) {

	checkURL, err := url.Parse("https://" + target + "/api/api_version")
	if err != nil {
		return nil, err
	}
	restversion, err := getAPIVersion(checkURL.String())
	if err != nil {
		return nil, err
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	c := &FlashBladeClient{Target: target, RestVersion: restversion, APIToken: apiToken}
	c.client = &http.Client{Transport: tr}

	err = c.login()
	if err != nil {
		return nil, err
	}

	return c, err
}
