package awsx

import (
	"strings"
	"testing"
)

const sampleConfig = `
[default]
region = us-east-1
output = json

# a comment
[profile work]
sso_session = corp
sso_account_id = 111122223333
sso_role_name = Developer
region = ap-south-1

[sso-session corp]
sso_start_url = https://corp.awsapps.com/start
sso_region = us-east-1

[profile legacy]
region = eu-west-1
role_arn = arn:aws:iam::444455556666:role/Ops
source_profile = default

[services my-services]
ec2 =
  endpoint_url = http://localhost:4566
`

const sampleCreds = `
[default]
aws_access_key_id = AKIAEXAMPLE
aws_secret_access_key = secret

[scratch]
aws_access_key_id = AKIAOTHER
aws_secret_access_key = secret2
region = us-west-2
`

func TestParseProfiles(t *testing.T) {
	got := parseProfiles(sampleConfig, sampleCreds)

	if len(got) != 4 {
		t.Fatalf("want 4 profiles, got %d: %+v", len(got), got)
	}
	if got[0].Name != "default" {
		t.Errorf("default should sort first, got %q", got[0].Name)
	}

	byName := map[string]Profile{}
	for _, p := range got {
		byName[p.Name] = p
	}

	for _, tc := range []struct {
		name     string
		region   string
		sso      bool
		inConfig bool
		inCreds  bool
	}{
		{"default", "us-east-1", false, true, true},
		{"work", "ap-south-1", true, true, false},
		{"legacy", "eu-west-1", false, true, false},
		{"scratch", "us-west-2", false, false, true},
	} {
		p, ok := byName[tc.name]
		if !ok {
			t.Errorf("missing profile %q", tc.name)
			continue
		}
		if p.Region != tc.region {
			t.Errorf("%s region = %q, want %q", tc.name, p.Region, tc.region)
		}
		if p.SSO != tc.sso {
			t.Errorf("%s SSO = %v, want %v", tc.name, p.SSO, tc.sso)
		}
		if p.InConfig != tc.inConfig || p.InCreds != tc.inCreds {
			t.Errorf("%s source = config:%v creds:%v, want config:%v creds:%v",
				tc.name, p.InConfig, p.InCreds, tc.inConfig, tc.inCreds)
		}
	}

	if _, ok := byName["corp"]; ok {
		t.Error("sso-session section must not become a profile")
	}
	if _, ok := byName["my-services"]; ok {
		t.Error("services section must not become a profile")
	}
}

func TestParseProfilesEmpty(t *testing.T) {
	if got := parseProfiles("", ""); len(got) != 0 {
		t.Fatalf("want no profiles, got %+v", got)
	}
}

func TestConfigSectionProfile(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"default", "default", true},
		{"profile work", "work", true},
		{"profile   spaced  ", "spaced", true},
		{"sso-session corp", "", false},
		{"services my-services", "", false},
		{"profile", "", false},
	} {
		got, ok := configSectionProfile(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("configSectionProfile(%q) = (%q,%v), want (%q,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseINIInlineComment(t *testing.T) {
	secs := parseINI(stringsReader("[default]\nregion = us-east-1 # inline\n"))
	if len(secs) != 1 || secs[0].Keys["region"] != "us-east-1" {
		t.Fatalf("got %+v", secs)
	}
}

func TestNormalizeArch(t *testing.T) {
	for in, want := range map[string]string{
		"x86_64": "amd64",
		"arm64":  "arm64",
		"i386":   "386",
		"weird":  "weird",
	} {
		if got := normalizeArch(in); got != want {
			t.Errorf("normalizeArch(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRegionOptionsPutsProfileRegionFirst(t *testing.T) {
	opts := regionOptions("ap-south-1")
	if opts[0] != "ap-south-1" {
		t.Fatalf("first option = %q", opts[0])
	}
	seen := map[string]int{}
	for _, r := range opts {
		seen[r]++
	}
	if seen["ap-south-1"] != 1 {
		t.Errorf("ap-south-1 appears %d times", seen["ap-south-1"])
	}
}

func stringsReader(s string) *strings.Reader { return strings.NewReader(s) }
