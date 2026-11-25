package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// callMCPTool runs one tool of the tracker's MCP server in process and returns
// its structured result as indented JSON. It lets a script or an assistant
// without the MCP connection use the same tools from the command line:
//
//	job-review-mcp.exe -db <db> -call get_application_kit -args "{\"job_id\":62}"
//	job-review-mcp.exe -db <db> -call save_tailored_resume -args-file args.json
func callMCPTool(db *sql.DB, name string, rawArgs []byte) (string, error) {
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := newMCPServer(db).Connect(ctx, st, nil); err != nil {
		return "", err
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "job-review-cli"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		return "", err
	}
	defer cs.Close()
	if name == "list" {
		tools, err := cs.ListTools(ctx, nil)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for _, t := range tools.Tools {
			fmt.Fprintf(&b, "%s: %s\n", t.Name, firstLine(t.Description))
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
	var args map[string]any
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", fmt.Errorf("arguments are not a JSON object: %w", err)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", err
	}
	if res.IsError {
		var msgs []string
		for _, c := range res.Content {
			if t, ok := c.(*mcp.TextContent); ok {
				msgs = append(msgs, t.Text)
			}
		}
		return "", fmt.Errorf("%s failed: %s", name, strings.Join(msgs, "; "))
	}
	b, err := json.MarshalIndent(res.StructuredContent, "", "  ")
	return string(b), err
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
