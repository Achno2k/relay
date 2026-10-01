package awsx

import (
	"errors"
	"strings"
	"testing"
)

const ec2Denial = `operation error EC2: DescribeInstances, https response error StatusCode: 403, ` +
	`RequestID: 1c0d1a2b-3c4d-5e6f-7081-92a3b4c5d6e7, api error UnauthorizedOperation: ` +
	`You are not authorized to perform this operation. User: arn:aws:iam::111122223333:user/aman ` +
	`is not authorized to perform: ec2:DescribeInstances because no identity-based policy allows it.`

const stsDenial = `operation error STS: GetCallerIdentity, https response error StatusCode: 403, ` +
	`api error AccessDenied: User: arn:aws:iam::111122223333:assumed-role/BuildRole/i-0abc ` +
	`is not authorized to perform: sts:GetCallerIdentity`

func TestARNFromError(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  string
		want string
	}{
		{"ec2 user", ec2Denial, "arn:aws:iam::111122223333:user/aman"},
		{"sts assumed role", stsDenial, "arn:aws:iam::111122223333:assumed-role/BuildRole/i-0abc"},
		{"trailing period", "User: arn:aws:iam::1:user/bo.", "arn:aws:iam::1:user/bo"},
		{"govcloud partition", "arn:aws-us-gov:iam::1:user/bo is not authorized", "arn:aws-us-gov:iam::1:user/bo"},
		{"no arn", "api error AccessDenied: nope", ""},
	} {
		if got := arnFromError(tc.msg); got != tc.want {
			t.Errorf("%s: arnFromError = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestIAMUserName(t *testing.T) {
	for _, tc := range []struct {
		arn  string
		want string
	}{
		{"arn:aws:iam::111122223333:user/aman", "aman"},
		{"arn:aws:iam::111122223333:user/eng/team/aman", "aman"},
		{"arn:aws-us-gov:iam::1:user/bo", "bo"},
		{"arn:aws:iam::111122223333:assumed-role/BuildRole/i-0abc", ""},
		{"arn:aws:iam::111122223333:role/BuildRole", ""},
		{"arn:aws:sts::111122223333:user/aman", ""},
		{"arn:aws:iam::111122223333:user/", ""},
		{"", ""},
		{"not-an-arn", ""},
	} {
		if got := iamUserName(tc.arn); got != tc.want {
			t.Errorf("iamUserName(%q) = %q, want %q", tc.arn, got, tc.want)
		}
	}
}

func TestDeniedAction(t *testing.T) {
	if got := deniedAction(ec2Denial); got != "ec2:DescribeInstances" {
		t.Errorf("deniedAction(ec2) = %q", got)
	}
	if got := deniedAction(stsDenial); got != "sts:GetCallerIdentity" {
		t.Errorf("deniedAction(sts) = %q", got)
	}
	if got := deniedAction("some other failure"); got != "" {
		t.Errorf("deniedAction(other) = %q, want empty", got)
	}
}

func TestIsDenied(t *testing.T) {
	if !isDenied(errors.New(ec2Denial)) {
		t.Error("UnauthorizedOperation should count as denied")
	}
	if !isDenied(errors.New(stsDenial)) {
		t.Error("AccessDenied should count as denied")
	}
	if !isDenied(errors.New("api error AccessDeniedException: nope")) {
		t.Error("AccessDeniedException should count as denied")
	}
	if isDenied(nil) {
		t.Error("nil is not a denial")
	}
	if isDenied(errors.New("operation error EC2: DescribeInstances, request timed out")) {
		t.Error("a timeout is not a denial")
	}
}

func TestPutUserPolicyCmd(t *testing.T) {
	got := putUserPolicyCmd("aman")
	want := "aws iam put-user-policy --user-name aman --policy-name agents-cli-read " +
		"--policy-document file://agents-cli-policy.json"
	if got != want {
		t.Errorf("putUserPolicyCmd = %q, want %q", got, want)
	}
}

func TestExplainDeniedPassesThroughOtherErrors(t *testing.T) {
	plain := errors.New("request timed out")
	if got := explainDenied(plain, "ec2:DescribeInstances"); got != plain {
		t.Errorf("non-denial error was rewritten to %v", got)
	}
	if got := explainDenied(nil, "ec2:DescribeInstances"); got != nil {
		t.Errorf("nil became %v", got)
	}
}

func TestDeniedErrorMessage(t *testing.T) {
	e := &DeniedError{Action: "ec2:DescribeInstances", ARN: "arn:aws:iam::1:user/aman"}
	if !strings.Contains(e.Error(), "ec2:DescribeInstances") {
		t.Errorf("Error() = %q", e.Error())
	}
	if got := (&DeniedError{}).Error(); got != "missing IAM permissions" {
		t.Errorf("empty DeniedError.Error() = %q", got)
	}
}

func TestIAMPolicyJSONMatchesDoc(t *testing.T) {
	for _, want := range []string{
		"ec2:DescribeInstances",
		"sts:GetCallerIdentity",
		"ssm:DescribeInstanceInformation",
		"ssm:StartSession",
		`"Version": "2012-10-17"`,
	} {
		if !strings.Contains(IAMPolicyJSON, want) {
			t.Errorf("embedded policy is missing %q", want)
		}
	}
}
