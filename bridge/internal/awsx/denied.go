package awsx

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Achno2k/agents-cli/internal/ui"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/smithy-go"
)

// IAMPolicyJSON is the minimal policy agents-cli needs. It is the same
// document as docs/iam-policy.json, embedded so the CLI can print it without
// the repo checked out.
const IAMPolicyJSON = `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "AgentsCliRead",
      "Effect": "Allow",
      "Action": [
        "ec2:DescribeInstances",
        "sts:GetCallerIdentity",
        "ssm:DescribeInstanceInformation"
      ],
      "Resource": "*"
    },
    {
      "Sid": "AgentsCliSsmTransportOptional",
      "Effect": "Allow",
      "Action": ["ssm:StartSession"],
      "Resource": "*"
    }
  ]
}`

// DeniedError means the AWS call was rejected for lack of IAM permissions.
// The fix has already been printed by the time this is returned.
type DeniedError struct {
	Action string // e.g. "ec2:DescribeInstances"
	ARN    string // the principal from the error, when AWS named one
}

func (e *DeniedError) Error() string {
	if e.Action == "" {
		return "missing IAM permissions"
	}
	return "missing IAM permissions for " + e.Action
}

// deniedCodes are the API error codes AWS uses for an authorisation failure.
var deniedCodes = []string{
	"UnauthorizedOperation",
	"AccessDeniedException",
	"AccessDeniedFault",
	"AccessDenied",
	"UnauthorizedAccess",
	"UnauthorizedException",
	"AuthorizationError",
}

// isDenied reports whether the error is an IAM authorisation failure.
func isDenied(err error) bool {
	if err == nil {
		return false
	}
	var ae smithy.APIError
	if errors.As(err, &ae) {
		for _, code := range deniedCodes {
			if ae.ErrorCode() == code {
				return true
			}
		}
	}
	var re *awshttp.ResponseError
	if errors.As(err, &re) && re.HTTPStatusCode() == 403 {
		return true
	}
	msg := err.Error()
	for _, code := range deniedCodes {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

var (
	arnRe    = regexp.MustCompile(`arn:aws[0-9a-zA-Z-]*:[0-9a-zA-Z-]*:[^:\s]*:[0-9]*:[^\s]+`)
	actionRe = regexp.MustCompile(`to perform:\s*([a-zA-Z0-9-]+:[A-Za-z0-9]+)`)
)

// arnFromError pulls the principal ARN out of an AWS denial message.
func arnFromError(msg string) string {
	m := arnRe.FindString(msg)
	return strings.TrimRight(m, ".,;:)\"'")
}

// iamUserName returns the user name of an IAM user ARN, or "" for a role,
// assumed role or anything else.
func iamUserName(arn string) string {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 || parts[2] != "iam" {
		return ""
	}
	resource, ok := strings.CutPrefix(parts[5], "user/")
	if !ok || resource == "" {
		return ""
	}
	if i := strings.LastIndex(resource, "/"); i >= 0 {
		resource = resource[i+1:]
	}
	return resource
}

// deniedAction returns the "service:Action" the caller was denied, if named.
func deniedAction(msg string) string {
	if m := actionRe.FindStringSubmatch(msg); len(m) == 2 {
		return m[1]
	}
	return ""
}

// putUserPolicyCmd is the one-liner that attaches the policy to an IAM user.
func putUserPolicyCmd(user string) string {
	return "aws iam put-user-policy --user-name " + user +
		" --policy-name agents-cli-read --policy-document file://agents-cli-policy.json"
}

// explainDenied turns an IAM denial into a printed explanation plus a short
// error. Any other error is passed through untouched.
func explainDenied(err error, fallbackAction string) error {
	if err == nil || !isDenied(err) {
		return err
	}
	msg := err.Error()
	arn := arnFromError(msg)
	action := deniedAction(msg)
	if action == "" {
		action = fallbackAction
	}

	who := arn
	if who == "" {
		who = "this AWS profile"
	}
	ui.Fail(fmt.Sprintf("%s is not allowed to call %s", who, action))

	ui.Info("Attach the minimal policy below, also kept at docs/iam-policy.json in the agents-cli repo:")
	ui.Code(IAMPolicyJSON)
	if user := iamUserName(arn); user != "" {
		ui.Info("Save it as agents-cli-policy.json, then run:")
		ui.Code(putUserPolicyCmd(user))
	} else {
		ui.Info("Attach it to the role behind this profile, or ask whoever owns the account to.")
	}

	return &DeniedError{Action: action, ARN: arn}
}
