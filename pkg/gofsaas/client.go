package gofsaas

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/your-org/gofsaas/pkg/socket"
)

// Client sends requests to a running gofsaas socket server.
type Client struct {
	sockPath string
}

// NewClient creates a Client connected to the given Unix socket path.
func NewClient(sockPath string) *Client {
	return &Client{sockPath: sockPath}
}

// Exists checks whether an S3 object exists for the given absolute path.
func (c *Client) Exists(path string) (socket.Response, error) {
	return c.send(socket.Request{Op: "exists", Path: path})
}

// Fetch starts a non-blocking download of the object at path into the local
// cache. It returns immediately after the download has been initiated.
func (c *Client) Fetch(path string) (socket.Response, error) {
	return c.send(socket.Request{Op: "fetch", Path: path})
}

// FetchWait downloads the object at path into the local cache and blocks
// until the download is complete.
func (c *Client) FetchWait(path string) (socket.Response, error) {
	return c.send(socket.Request{Op: "fetch", Path: path, Wait: true})
}

// Clean removes the cached object at path and resets its state.
func (c *Client) Clean(path string) (socket.Response, error) {
	return c.send(socket.Request{Op: "clean", Path: path})
}

// Status requests a status summary from the server.
func (c *Client) Status() (socket.Response, error) {
	return c.send(socket.Request{Op: "status", Path: ""})
}

func (c *Client) send(req socket.Request) (socket.Response, error) {
	conn, err := net.Dial("unix", c.sockPath)
	if err != nil {
		return socket.Response{}, fmt.Errorf("gofsaas.Client dial %s: %w", c.sockPath, err)
	}
	defer conn.Close()

	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		return socket.Response{}, fmt.Errorf("gofsaas.Client encode: %w", err)
	}

	var resp socket.Response
	dec := json.NewDecoder(conn)
	if err := dec.Decode(&resp); err != nil {
		return socket.Response{}, fmt.Errorf("gofsaas.Client decode: %w", err)
	}
	return resp, nil
}
