package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/KchaiI/slipway/internal/api"
)

// Client is a thin HTTP client for the minato-server REST API.
type Client struct {
	Base string
	http *http.Client
}

func NewClient(base string) *Client {
	return &Client{Base: base, http: &http.Client{}}
}

// do performs a JSON request. A nil out skips response decoding.
func (c *Client) do(method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.Base+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach minato-server at %s: %w", c.Base, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		var apiErr api.Error
		if json.NewDecoder(resp.Body).Decode(&apiErr) == nil && apiErr.Error != "" {
			return fmt.Errorf("%s", apiErr.Error)
		}
		return fmt.Errorf("server returned %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) CreateApp(name string) (*api.App, error) {
	var app api.App
	err := c.do("POST", "/v1/apps", api.CreateAppRequest{Name: name}, &app)
	return &app, err
}

func (c *Client) ListApps() ([]api.App, error) {
	var apps []api.App
	err := c.do("GET", "/v1/apps", nil, &apps)
	return apps, err
}

func (c *Client) AppStatus(app string) (*api.AppStatus, error) {
	var st api.AppStatus
	err := c.do("GET", "/v1/apps/"+app, nil, &st)
	return &st, err
}

func (c *Client) DestroyApp(app string) error {
	return c.do("DELETE", "/v1/apps/"+app, nil, nil)
}

func (c *Client) Releases(app string) ([]api.Release, error) {
	var rels []api.Release
	err := c.do("GET", "/v1/apps/"+app+"/releases", nil, &rels)
	return rels, err
}

func (c *Client) DeployImage(app, image string) (*api.Release, error) {
	var rel api.Release
	err := c.do("POST", "/v1/apps/"+app+"/deployments", api.DeployImageRequest{Image: image}, &rel)
	return &rel, err
}
