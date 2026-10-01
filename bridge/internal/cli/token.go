package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"relay/internal/config"
)

func init() {
	var rotate bool
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Print the bearer token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToken(rotate)
		},
	}
	cmd.Flags().BoolVar(&rotate, "rotate", false, "replace the token; paired phones must pair again")
	Register(cmd)
}

func runToken(rotate bool) error {
	if rotate {
		t, err := config.RotateToken()
		if err != nil {
			return err
		}
		fmt.Println(t)
		fmt.Fprintln(os.Stderr, "rotated; restart `relay serve` and re-run `relay pair`")
		return nil
	}
	t, err := config.LoadToken()
	if err != nil {
		return err
	}
	fmt.Println(t)
	return nil
}
