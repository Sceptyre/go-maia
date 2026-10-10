package cmd

import (
	"fmt"
	"net"
	"net/http"

	"github.com/sceptyre/maia/internal/web"
	"github.com/spf13/cobra"
)

var (
	webPort  int
	webAddr  string
	webToken string
)

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Start the web UI",
	Long: `Start a local web UI for maia.

The server manages multiple projects (git repositories): it auto-registers
the current repository, and more can be added from the dashboard. Use it to
create change requests, edit change.md, review research and plan documents,
run AI operations with live output, inspect diffs, import GitHub issues as
change requests, open draft PRs, and merge or clean up.

By default it binds to 127.0.0.1 — do not expose it publicly without a token.

Examples:
  maia web                 # http://127.0.0.1:7878
  maia web --port 3000
  maia web --addr 0.0.0.0 --token my-secret`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		loopback := isLoopback(webAddr)
		if !loopback && webToken == "" {
			return fmt.Errorf("--token is required when binding to a non-loopback address")
		}

		srv := web.New(webToken, webAddr, webPort)

		addr := fmt.Sprintf("%s:%d", webAddr, webPort)
		url := fmt.Sprintf("http://%s:%d/", webAddr, webPort)
		fmt.Printf("Maia web UI: %s\n", url)
		if webToken != "" {
			fmt.Printf("Token:       %s (or pass ?token=... to the URL)\n", webToken)
		}
		if !loopback {
			fmt.Println("WARNING: binding to a non-loopback address — anyone with the token can run commands.")
		}

		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("cannot listen on %s: %w", addr, err)
		}
		return http.Serve(ln, srv.Handler())
	},
}

func isLoopback(host string) bool {
	if host == "" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func init() {
	webCmd.Flags().IntVarP(&webPort, "port", "p", 7878, "Port to listen on")
	webCmd.Flags().StringVarP(&webAddr, "addr", "a", "127.0.0.1", "Address to bind")
	webCmd.Flags().StringVar(&webToken, "token", "", "Require this token for all requests (mandatory for non-loopback binds)")
	rootCmd.AddCommand(webCmd)
}
