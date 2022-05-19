package cli

import (
	"github.com/acorn-io/acorn/pkg/server"
	cli "github.com/rancher/wrangler-cli"
	"github.com/spf13/cobra"
)

var (
	apiServer = server.New()
)

func NewApiServer() *cobra.Command {
	api := &APIServer{}
	cmd := cli.Command(api, cobra.Command{
		Use:          "api-server [flags] [APP_NAME...]",
		SilenceUsage: true,
		Short:        "Run api-server",
		Hidden:       true,
	})
	apiServer.AddFlags(cmd.Flags())
	return cmd
}

type APIServer struct {
}

func (a *APIServer) Run(cmd *cobra.Command, args []string) error {
	cfg, err := apiServer.NewConfig(cmd.Version)
	if err != nil {
		return err
	}

	return apiServer.Run(cmd.Context(), cfg)
}
