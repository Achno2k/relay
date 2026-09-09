// Package awsx picks the AWS profile, region and EC2 instance.
// Owner: session "aws".
package awsx

import (
	"context"

	"github.com/you/agents-cli/internal/config"
)

// Instance is one running EC2 instance as shown in the picker.
type Instance struct {
	ID        string
	Name      string
	Type      string
	PublicIP  string
	PublicDNS string
	Arch      string // x86_64 | arm64
	State     string
	SSMReady  bool
}

// PickTarget runs the full interactive flow: profile → creds → region →
// instance → transport. Returns the values to save in config.
func PickTarget(ctx context.Context) (config.AWS, config.Box, error) { panic("TODO awsx") }

// InstanceArch returns the CPU arch of the configured instance ("amd64"|"arm64"
// in GOARCH terms) so the laptop can cross-compile the box binary.
func InstanceArch(ctx context.Context, a config.AWS) (string, error) { panic("TODO awsx") }
