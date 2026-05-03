package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
)

// Client communicates with the mydockerd daemon
type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{
		http: &http.Client{
			Transport: &http.Transport{
				Dial: func(_, _ string) (net.Conn, error) {
					return net.Dial("unix", SocketPath)
				},
			},
		},
	}
}

func (c *Client) Ping() error {
	resp, err := c.http.Get("http://daemon/ping")
	if err != nil {
		return fmt.Errorf("daemon not running (start with: sudo mydockerd)")
	}
	defer resp.Body.Close()
	return nil
}

func (c *Client) CreateContainer(req *CreateContainerRequest) (*CreateContainerResponse, error) {
	var resp CreateContainerResponse
	err := c.post("/containers", req, &resp)
	return &resp, err
}

func (c *Client) StartContainer(id string) error {
	return c.postRaw(fmt.Sprintf("/containers/%s/start", id), nil)
}

func (c *Client) StopContainer(id string) error {
	return c.postRaw(fmt.Sprintf("/containers/%s/stop", id), nil)
}

func (c *Client) RemoveContainer(id string, force bool) error {
	url := fmt.Sprintf("http://daemon/containers/%s", id)
	if force {
		url += "?force=true"
	}
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return readError(resp)
	}
	return nil
}

func (c *Client) ListContainers(all bool) ([]*ContainerInfo, error) {
	url := "http://daemon/containers"
	if all {
		url += "?all=true"
	}
	resp, err := c.http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var containers []*ContainerInfo
	return containers, json.NewDecoder(resp.Body).Decode(&containers)
}

func (c *Client) InspectContainer(id string) (*ContainerInfo, error) {
	resp, err := c.http.Get(fmt.Sprintf("http://daemon/containers/%s", id))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("container not found: %s", id)
	}
	var info ContainerInfo
	return &info, json.NewDecoder(resp.Body).Decode(&info)
}

func (c *Client) GetLogs(id string) (string, error) {
	resp, err := c.http.Get(fmt.Sprintf("http://daemon/containers/%s/logs", id))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return string(data), err
}

func (c *Client) GetStats(id string) (*StatsResponse, error) {
	resp, err := c.http.Get(fmt.Sprintf("http://daemon/containers/%s/stats", id))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var stats StatsResponse
	return &stats, json.NewDecoder(resp.Body).Decode(&stats)
}

func (c *Client) PullImage(image string) error {
	return c.postRaw("/images/pull", &PullRequest{Image: image})
}

func (c *Client) ListImages() ([]*ImageInfo, error) {
	resp, err := c.http.Get("http://daemon/images")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var images []*ImageInfo
	return images, json.NewDecoder(resp.Body).Decode(&images)
}

func (c *Client) post(path string, body interface{}, out interface{}) error {
	data, _ := json.Marshal(body)
	resp, err := c.http.Post("http://daemon"+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return readError(resp)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *Client) postRaw(path string, body interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}
	resp, err := c.http.Post("http://daemon"+path, "application/json", bodyReader)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return readError(resp)
	}
	return nil
}

func readError(resp *http.Response) error {
	var errResp ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return fmt.Errorf("%s", errResp.Error)
}
