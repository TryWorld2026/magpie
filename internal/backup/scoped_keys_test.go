package backup

import (
	"encoding/json"
	"testing"

	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/library"
	"github.com/yetone/magpie/internal/settings"
)

func TestRestoreLibraryKeysScope(t *testing.T) {
	for _, policy := range []string{"keyless", "full", "settings", "providers", "library"} {
		for _, selected := range []bool{false, true} {
			name := policy + "/excluded"
			if selected {
				name = policy + "/selected"
			}
			t.Run(name, func(t *testing.T) {
				home(t)
				appdir.UseExecutable("")
				cur := settings.Load()
				cur.GitHubToken = "fixture-local-github"
				if err := settings.Save(cur); err != nil {
					t.Fatal(err)
				}
				if _, err := library.SaveServer("", library.Server{Name: "github", Transport: "stdio", Command: "fixture-mcp",
					Env: map[string]string{"GITHUB_TOKEN": "fixture-local-mcp"}, Agents: []string{}}); err != nil {
					t.Fatal(err)
				}
				if _, err := library.SaveServer("", library.Server{Name: "headers", Transport: "http", URL: "https://mcp.example.com",
					Headers: map[string]string{"Authorization": "fixture-local-header"}, Agents: []string{}}); err != nil {
					t.Fatal(err)
				}
				b := Bundle{Version: 1, Keys: policy == "full", Library: &library.Bundle{MCP: []*library.Server{{Name: "github", Transport: "stdio", Command: "changed-mcp",
					Env: map[string]string{"GITHUB_TOKEN": ""}, Agents: []string{}}, {Name: "headers", Transport: "http", URL: "https://mcp.example.com",
					Headers: map[string]string{"Authorization": ""}, Agents: []string{}}}}}
				if policy != "keyless" && policy != "full" {
					// Read a serialized scoped bundle, as an imported backup would.
					if err := json.Unmarshal([]byte(`{"`+policy+`Keys":true}`), &b); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := Restore(b, Parts{Library: selected}); err != nil {
					t.Fatal(err)
				}
				got, err := library.Collect()
				if err != nil {
					t.Fatal(err)
				}
				wantToken, wantHeader, wantCommand := "fixture-local-mcp", "fixture-local-header", "fixture-mcp"
				if selected {
					wantCommand = "changed-mcp"
					if policy == "full" || policy == "library" {
						wantToken, wantHeader = "", ""
					}
				}
				if got.MCP[0].Command != wantCommand || got.MCP[0].Env["GITHUB_TOKEN"] != wantToken || got.MCP[1].Headers["Authorization"] != wantHeader {
					t.Errorf("library restoration ignored its selected credential scope: command=%q, token=%q, header=%q", got.MCP[0].Command, got.MCP[0].Env["GITHUB_TOKEN"], got.MCP[1].Headers["Authorization"])
				}
				if settings.Load().GitHubToken != cur.GitHubToken {
					t.Error("restoring the library changed an unselected settings credential")
				}
			})
		}
	}
}
