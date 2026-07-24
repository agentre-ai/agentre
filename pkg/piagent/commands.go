package piagent

import (
	"context"
	"encoding/json"
	"fmt"
)

// Command is one command resolved by Pi for the client's cwd and configuration.
type Command struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
	Location    string `json:"location,omitempty"`
	Path        string `json:"path,omitempty"`
}

// ListCommands asks a short-lived Pi RPC process for its effective extension,
// prompt-template, and skill commands. Pi owns user/project/package precedence.
func (c *Client) ListCommands(ctx context.Context) ([]Command, error) {
	proc, err := c.startRPC(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = proc.terminate(context.Background(), c.killGrace) }()

	if err := proc.writeJSON(map[string]any{"type": "get_commands"}); err != nil {
		return nil, err
	}
	for proc.lines.Scan() {
		var response rpcResponse
		if err := json.Unmarshal(proc.lines.Bytes(), &response); err != nil {
			return nil, fmt.Errorf("piagent decode get_commands response: %w", err)
		}
		if response.Type != "response" || response.Command != "get_commands" {
			continue
		}
		if !response.Success {
			return nil, failureResponseError(response)
		}
		var data struct {
			Commands []Command `json:"commands"`
		}
		if err := json.Unmarshal(response.Data, &data); err != nil {
			return nil, fmt.Errorf("piagent decode get_commands data: %w", err)
		}
		return data.Commands, nil
	}
	return nil, processDeadOrScanError(proc)
}
