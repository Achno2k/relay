// Package sshx runs commands on the box by shelling out to the system ssh.
// Owner: session "aws". SSM transport = ssh with a ProxyCommand.
package sshx

import (
	"context"
	"io"
)

type Target struct {
	Host       string
	User       string
	Transport  string // ssh | ssm
	KeyPath    string
	InstanceID string
	Profile    string
	Region     string
}

type Runner interface {
	// Run executes bash on the box, streams stdout/stderr, returns exit error.
	Run(ctx context.Context, script string, stdout, stderr io.Writer) error
	// Interactive attaches the local tty to a remote command (login flows,
	// herdr --remote).
	Interactive(ctx context.Context, command string) error
	// Copy uploads a local file to a remote path.
	Copy(ctx context.Context, localPath, remotePath string) error
	// Args returns the ssh argv prefix for callers that must exec ssh themselves.
	Args() []string
}

func New(t Target) Runner { panic("TODO sshx") }
