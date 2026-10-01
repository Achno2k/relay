// Command relay is the bridge between herdr and the Relay iOS app, and the
// laptop CLI that sets up a box for it.
package main

import "relay/internal/cli"

func main() { cli.Execute() }
