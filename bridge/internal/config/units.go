package config

import (
	"fmt"
	"html"
	"path/filepath"
	"strings"
)

const LaunchAgentLabel = "com.relay.bridge"

// servicePATH is what launchd gives `relay serve` (and so the agents' CLIs it spawns).
const servicePATH = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

func LaunchAgentPath() string {
	return filepath.Join(userHome(), "Library/LaunchAgents", LaunchAgentLabel+".plist")
}

// LaunchAgentPlist is the LaunchAgent for `relay serve`. At login Tailscale may not be up yet;
// --require-tailscale exits so KeepAlive retries until it is. Keys are sorted, like
// PropertyListSerialization writes them.
func LaunchAgentPlist(executable string, port int) []byte {
	log := html.EscapeString(LogPath())
	var args strings.Builder
	for _, a := range []string{executable, "serve", "--port", fmt.Sprint(port), "--require-tailscale"} {
		fmt.Fprintf(&args, "\t\t<string>%s</string>\n", html.EscapeString(a))
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>` + servicePATH + `</string>
	</dict>
	<key>KeepAlive</key>
	<true/>
	<key>Label</key>
	<string>` + LaunchAgentLabel + `</string>
	<key>ProgramArguments</key>
	<array>
` + args.String() + `	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>StandardErrorPath</key>
	<string>` + log + `</string>
	<key>StandardOutPath</key>
	<string>` + log + `</string>
</dict>
</plist>
`)
}
