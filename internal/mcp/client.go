// Package mcp implements a minimal Model Context Protocol client.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// Server represents an MCP server config.
type Server struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	URL     string            `json:"url,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// Client is an MCP client connection.
type Client struct {
	server Server
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
	id     int
	tools  map[string]ToolDef
}

// ToolDef is an MCP tool definition.
type ToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
}

// Connect starts an MCP server and initializes.
func Connect(ctx context.Context, srv Server) (*Client, error) {
	c := &Client{server: srv, id: 0, tools: make(map[string]ToolDef)}
	if srv.Command == "" && srv.URL == "" {
		return nil, fmt.Errorf("server %s: need either Command (stdio) or URL (http)", srv.Name)
	}

	if srv.Command != "" {
		cmd := exec.CommandContext(ctx, srv.Command, srv.Args...)
		for k, v := range srv.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start %s: %w", srv.Name, err)
		}
		c.cmd = cmd
		c.stdin = stdin
		c.stdout = bufio.NewReader(stdout)
	}

	// Initialize
	if err := c.send(ctx, "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "jito",
			"version": "0.1.0",
		},
	}); err != nil {
		c.Close()
		return nil, fmt.Errorf("initialize: %w", err)
	}

	// List tools (best-effort)
	_ = c.send(ctx, "tools/list", nil)

	return c, nil
}

func (c *Client) send(ctx context.Context, method string, params interface{}) error {
	c.mu.Lock()
	c.id++
	id := c.id
	c.mu.Unlock()

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	data, _ := json.Marshal(req)
	if c.stdin != nil {
		if _, err := c.stdin.Write(append(data, '\n')); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("stdio not connected")
	}

	if c.stdout == nil {
		return fmt.Errorf("no stdout")
	}
	line, err := c.stdout.ReadString('\n')
	if err != nil {
		return err
	}

	var resp struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return fmt.Errorf("parse: %w (raw: %s)", err, strings.TrimSpace(line))
	}
	if resp.Error != nil {
		return fmt.Errorf("mcp error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	if method == "tools/list" && resp.Result != nil {
		var result struct {
			Tools []ToolDef `json:"tools"`
		}
		if err := json.Unmarshal(resp.Result, &result); err == nil {
			for _, tool := range result.Tools {
				c.tools[tool.Name] = tool
			}
		}
	}

	return nil
}

// Tools returns discovered MCP tools.
func (c *Client) Tools() map[string]ToolDef { return c.tools }

// Call invokes an MCP tool.
func (c *Client) Call(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	c.mu.Lock()
	c.id++
	id := c.id
	c.mu.Unlock()

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      name,
			"arguments": args,
		},
	}
	data, _ := json.Marshal(req)
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return "", err
	}

	line, err := c.stdout.ReadString('\n')
	if err != nil {
		return "", err
	}

	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return "", err
	}
	if resp.Error != nil {
		return "", fmt.Errorf("mcp: %s", resp.Error.Message)
	}
	return string(resp.Result), nil
}

// Close terminates the MCP server.
func (c *Client) Close() error {
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Process.Kill()
	}
	return nil
}

// Name returns the server name.
func (c *Client) Name() string { return c.server.Name }

// String returns a description.
func (c *Client) String() string {
	return fmt.Sprintf("mcp(name=%s, tools=%d)", c.server.Name, len(c.tools))
}