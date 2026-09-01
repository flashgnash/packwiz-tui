package main

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The embedded agent chat runs claude in print mode, which cannot show its
// own permission prompts. Instead we hand claude --permission-prompt-tool
// pointing at an MCP server served by THIS binary (`packwiz-tui mcp-approve
// <socket>`), which forwards every permission request over a unix socket to
// the TUI. The TUI pops an Allow/Always/Deny dialog and the verdict travels
// back: socket → MCP tool result → claude.

// ── MCP server side (runs as a claude subprocess) ────────────────────────────

// RunMCPApprove serves the minimal MCP stdio protocol with one tool
// ("approve") that bridges permission requests to the TUI's unix socket.
func RunMCPApprove(socketPath string) error {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	out := bufio.NewWriter(os.Stdout)
	respond := func(id json.RawMessage, result any) {
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
		out.Write(b)
		out.WriteByte('\n')
		out.Flush()
	}
	for sc.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil {
			continue
		}
		switch req.Method {
		case "initialize":
			respond(req.ID, map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "packwiz-tui", "version": "0.1"},
			})
		case "tools/list":
			respond(req.ID, map[string]any{"tools": []any{map[string]any{
				"name":        "approve",
				"description": "Ask the packwiz-tui user to approve or deny a tool call",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"tool_name": map[string]any{"type": "string"},
						"input":     map[string]any{"type": "object"},
					},
					"additionalProperties": true,
				},
			}}})
		case "tools/call":
			var p struct {
				Arguments json.RawMessage `json:"arguments"`
			}
			json.Unmarshal(req.Params, &p)
			respond(req.ID, map[string]any{"content": []any{
				map[string]any{"type": "text", "text": bridgeAsk(socketPath, p.Arguments)},
			}})
		case "ping":
			respond(req.ID, map[string]any{})
		default:
			if req.ID != nil {
				respond(req.ID, map[string]any{})
			}
		}
	}
	return sc.Err()
}

func bridgeAsk(socketPath string, payload []byte) string {
	const unavailable = `{"behavior":"deny","message":"packwiz-tui approval dialog unavailable"}`
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return unavailable
	}
	defer conn.Close()
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	conn.Write(append(payload, '\n'))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return unavailable
	}
	return strings.TrimSpace(line)
}

// ── TUI side ─────────────────────────────────────────────────────────────────

// approveReq is one pending permission request awaiting a user verdict.
type approveReq struct {
	ToolName string
	Input    json.RawMessage
	resp     chan string
}

type msgApproveReq struct{ req *approveReq }

// startApproveBridge opens the unix socket the MCP server dials into and
// writes the mcp-config file claude is pointed at. Returns the request
// channel and the config path.
func startApproveBridge(packDir string) (net.Listener, chan *approveReq, string, error) {
	dir, err := harnessDir(packDir, "")
	if err != nil {
		return nil, nil, "", err
	}
	sock := filepath.Join(dir, "approve.sock")
	os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return nil, nil, "", err
	}

	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	cfg := map[string]any{"mcpServers": map[string]any{
		"ptui": map[string]any{"command": self, "args": []string{"mcp-approve", sock}},
	}}
	cfgData, _ := json.Marshal(cfg)
	cfgPath := filepath.Join(dir, "mcp-approve.json")
	if err := os.WriteFile(cfgPath, cfgData, 0644); err != nil {
		ln.Close()
		return nil, nil, "", err
	}

	ch := make(chan *approveReq, 4)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				close(ch)
				return
			}
			go handleApproveConn(conn, ch)
		}
	}()
	return ln, ch, cfgPath, nil
}

func handleApproveConn(conn net.Conn, ch chan *approveReq) {
	defer conn.Close()
	line, err := bufio.NewReaderSize(conn, 1024*1024).ReadBytes('\n')
	if err != nil {
		return
	}
	var payload struct {
		ToolName string          `json:"tool_name"`
		Input    json.RawMessage `json:"input"`
	}
	json.Unmarshal(line, &payload)
	r := &approveReq{ToolName: payload.ToolName, Input: payload.Input, resp: make(chan string, 1)}
	ch <- r
	verdict := <-r.resp
	conn.Write([]byte(verdict + "\n"))
}

// ensureApproveBridge lazily starts the bridge; returns the pump command on
// first start.
func (a *App) ensureApproveBridge() tea.Cmd {
	if a.approveCh != nil {
		return nil
	}
	ln, ch, cfgPath, err := startApproveBridge(a.packDir)
	if err != nil {
		a.agentEntries = append(a.agentEntries, agentEntry{role: "error",
			text: "permission prompts unavailable: " + err.Error()})
		return nil
	}
	a.approveLn, a.approveCh, a.mcpConfigPath = ln, ch, cfgPath
	return a.waitApprove()
}

// waitApprove pumps the next bridged permission request into the update loop.
func (a *App) waitApprove() tea.Cmd {
	ch := a.approveCh
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return nil
		}
		return msgApproveReq{req: r}
	}
}

// resolveApprove answers the pending request and re-arms the pump.
func (a *App) resolveApprove(allow, always bool) tea.Cmd {
	r := a.approvePending
	if r == nil {
		return nil
	}
	a.approvePending = nil
	if always {
		addAgentAllowRule(a.packDir, approveRule(r))
	}
	if allow {
		input := r.Input
		if len(input) == 0 {
			input = []byte("{}")
		}
		v, _ := json.Marshal(map[string]any{"behavior": "allow", "updatedInput": json.RawMessage(input)})
		r.resp <- string(v)
	} else {
		r.resp <- `{"behavior":"deny","message":"denied by the user in packwiz-tui"}`
	}
	return a.waitApprove()
}

// approveRule derives a settings.json allow rule from a request — command
// prefix for Bash, plain tool name otherwise.
func approveRule(r *approveReq) string {
	if r.ToolName == "Bash" {
		var in struct {
			Command string `json:"command"`
		}
		json.Unmarshal(r.Input, &in)
		if tok := strings.Fields(in.Command); len(tok) > 0 {
			return "Bash(" + tok[0] + ":*)"
		}
	}
	return r.ToolName
}

// approveDetail renders the human-readable summary of what's being asked.
func approveDetail(r *approveReq) string {
	var in map[string]any
	json.Unmarshal(r.Input, &in)
	switch r.ToolName {
	case "Bash":
		if c, ok := in["command"].(string); ok {
			return c
		}
	case "Edit", "Write", "Read", "NotebookEdit":
		if p, ok := in["file_path"].(string); ok {
			return p
		}
	case "WebFetch":
		if u, ok := in["url"].(string); ok {
			return u
		}
	}
	b, _ := json.Marshal(in)
	return truncate(string(b), 200)
}

// addAgentAllowRule appends one allow rule to the pack's project-level
// claude settings (same merge behaviour as ensureAgentSettings).
func addAgentAllowRule(packDir, rule string) {
	path := filepath.Join(packDir, ".claude", "settings.json")
	var root map[string]any
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &root)
	}
	if root == nil {
		root = map[string]any{}
	}
	perms, _ := root["permissions"].(map[string]any)
	if perms == nil {
		perms = map[string]any{}
	}
	existing, _ := perms["allow"].([]any)
	for _, e := range existing {
		if s, ok := e.(string); ok && s == rule {
			return
		}
	}
	perms["allow"] = append(existing, rule)
	root["permissions"] = perms
	os.MkdirAll(filepath.Dir(path), 0755)
	if data, err := json.MarshalIndent(root, "", "  "); err == nil {
		os.WriteFile(path, append(data, '\n'), 0644)
	}
}
