package machine

import (
	"regexp"
	"strings"

	"golang.org/x/sys/unix"

	"relay/internal/api"
)

var platformUUID = regexp.MustCompile(`"IOPlatformUUID" = "([0-9A-Fa-f-]+)"`)

func current() api.Machine {
	model, err := unix.Sysctl("hw.model")
	if err != nil || model == "" {
		model = "Mac"
	}
	// The hardware UUID (what gethostuuid returns), lowercased like Swift's.
	id := hostname()
	if m := platformUUID.FindStringSubmatch(output("/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice")); m != nil {
		id = strings.ToLower(m[1])
	}
	name := output("/usr/sbin/scutil", "--get", "ComputerName")
	if name == "" {
		name = hostname()
	}
	os := "macOS"
	if v, err := unix.Sysctl("kern.osproductversion"); err == nil && v != "" {
		os += " " + strings.TrimSuffix(v, ".0")
	}
	return api.Machine{ID: id, Name: name, Kind: Kind(model, hasInternalBattery()), Model: model, OS: os}
}

func hasInternalBattery() bool {
	return strings.Contains(output("/usr/bin/pmset", "-g", "batt"), "InternalBattery")
}
