package awsx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Achno2k/agents-cli/internal/ui"
	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// LoadConfig builds an SDK config for a profile and region.
func LoadConfig(ctx context.Context, profile, region string) (aws.Config, error) {
	opts := []func(*awscfg.LoadOptions) error{}
	if profile != "" {
		opts = append(opts, awscfg.WithSharedConfigProfile(profile))
	}
	if region != "" {
		opts = append(opts, awscfg.WithRegion(region))
	}
	return awscfg.LoadDefaultConfig(ctx, opts...)
}

// CredsValid reports whether the config can call sts:GetCallerIdentity.
// An IAM denial is explained on the terminal and reduced to a DeniedError.
func CredsValid(ctx context.Context, cfg aws.Config) error {
	_, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	return explainDenied(err, "sts:GetCallerIdentity")
}

// EnsureCreds returns a usable config for the profile. If the credentials are
// missing or expired and the profile is an SSO profile, it runs
// `aws sso login --profile <name>` on the terminal and retries once.
func EnsureCreds(ctx context.Context, p Profile, region string) (aws.Config, error) {
	cfg, err := LoadConfig(ctx, p.Name, region)
	if err == nil {
		if err = CredsValid(ctx, cfg); err == nil {
			return cfg, nil
		}
	}
	var denied *DeniedError
	if errors.As(err, &denied) {
		return cfg, denied
	}
	if !p.SSO {
		return cfg, fmt.Errorf("profile %q has no usable credentials: %w", p.Name, err)
	}
	if _, lookErr := exec.LookPath("aws"); lookErr != nil {
		return cfg, fmt.Errorf("profile %q needs `aws sso login` but the aws CLI is not on PATH", p.Name)
	}
	if err := ssoLogin(ctx, p.Name); err != nil {
		return cfg, err
	}
	cfg, err = LoadConfig(ctx, p.Name, region)
	if err != nil {
		return cfg, err
	}
	if err := CredsValid(ctx, cfg); err != nil {
		if errors.As(err, &denied) {
			return cfg, denied
		}
		return cfg, fmt.Errorf("still no usable credentials for %q after sso login: %w", p.Name, err)
	}
	return cfg, nil
}

// ssoLogin runs the interactive SSO login for a profile on the user's terminal.
func ssoLogin(ctx context.Context, profile string) error {
	return runAWS(ctx, "sso", "login", "--profile", profile)
}

// runAWS runs an aws subcommand attached to the user's terminal.
func runAWS(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "aws", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("aws %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// requireAWSCLI reports a missing aws CLI with the install command.
func requireAWSCLI() error {
	if _, err := exec.LookPath("aws"); err != nil {
		ui.Fail("the aws CLI is not on PATH")
		ui.Code("brew install awscli")
		return errors.New("aws CLI not found")
	}
	return nil
}

// instanceInfo carries picker fields plus data only the picker flow needs.
type instanceInfo struct {
	Instance
	KeyName string
}

// ListInstances returns the running EC2 instances in the config's region,
// marked with whether SSM can reach them.
func ListInstances(ctx context.Context, cfg aws.Config) ([]Instance, error) {
	infos, err := listInstances(ctx, cfg)
	if err != nil {
		return nil, err
	}
	out := make([]Instance, len(infos))
	for i, in := range infos {
		out[i] = in.Instance
	}
	return out, nil
}

func listInstances(ctx context.Context, cfg aws.Config) ([]instanceInfo, error) {
	client := ec2.NewFromConfig(cfg)
	pager := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{{
			Name:   aws.String("instance-state-name"),
			Values: []string{"running"},
		}},
	})
	var out []instanceInfo
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, explainDenied(err, "ec2:DescribeInstances")
		}
		for _, res := range page.Reservations {
			for _, i := range res.Instances {
				out = append(out, instanceInfo{
					Instance: Instance{
						ID:        aws.ToString(i.InstanceId),
						Name:      tagValue(i.Tags, "Name"),
						Type:      string(i.InstanceType),
						PublicIP:  aws.ToString(i.PublicIpAddress),
						PublicDNS: aws.ToString(i.PublicDnsName),
						Arch:      string(i.Architecture),
						State:     stateName(i.State),
					},
					KeyName: aws.ToString(i.KeyName),
				})
			}
		}
	}
	managed, err := ssmManaged(ctx, cfg)
	if err == nil {
		for i := range out {
			out[i].SSMReady = managed[out[i].ID]
		}
	}
	return out, nil
}

// ssmManaged returns the set of instance IDs SSM currently reports as online.
func ssmManaged(ctx context.Context, cfg aws.Config) (map[string]bool, error) {
	client := ssm.NewFromConfig(cfg)
	pager := ssm.NewDescribeInstanceInformationPaginator(client, &ssm.DescribeInstanceInformationInput{})
	set := map[string]bool{}
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return set, err
		}
		for _, info := range page.InstanceInformationList {
			if info.PingStatus == "Online" {
				set[aws.ToString(info.InstanceId)] = true
			}
		}
	}
	return set, nil
}

func tagValue(tags []ec2types.Tag, key string) string {
	for _, t := range tags {
		if aws.ToString(t.Key) == key {
			return aws.ToString(t.Value)
		}
	}
	return ""
}

func stateName(s *ec2types.InstanceState) string {
	if s == nil {
		return ""
	}
	return string(s.Name)
}

// normalizeArch converts an EC2 architecture to its GOARCH name.
func normalizeArch(ec2Arch string) string {
	switch strings.ToLower(ec2Arch) {
	case "x86_64", "x86_64_mac", "amd64":
		return "amd64"
	case "arm64", "arm64_mac", "aarch64":
		return "arm64"
	case "i386":
		return "386"
	default:
		return ec2Arch
	}
}

// describeInstance fetches one instance by ID.
func describeInstance(ctx context.Context, cfg aws.Config, id string) (ec2types.Instance, error) {
	out, err := ec2.NewFromConfig(cfg).DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{id},
	})
	if err != nil {
		return ec2types.Instance{}, err
	}
	for _, res := range out.Reservations {
		for _, i := range res.Instances {
			return i, nil
		}
	}
	return ec2types.Instance{}, errors.New("instance " + id + " not found")
}

// hasSessionManagerPlugin reports whether the SSM session-manager-plugin is installed.
func hasSessionManagerPlugin() bool {
	_, err := exec.LookPath("session-manager-plugin")
	return err == nil
}
