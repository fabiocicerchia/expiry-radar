package source

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// AWSSource covers the three AWS things that quietly stop working: ACM
// certificates, IAM access keys nobody rotated, and secrets whose rotation
// schedule has slipped.
//
// Every call below is a List/Describe. The exact set is in
// docs/iam-readonly-policy.json — ship it, and say so, because credential
// sprawl is the adoption blocker for security teams.
//
// IAM access keys do not have an expiry date: AWS will happily serve a
// five-year-old key. MaxKeyAge turns "age" into the deadline the rest of the
// tool can rank, which is the whole reason the secret-rotation calendar merged
// into this binary.
type AWSSource struct {
	Region     string
	Profile    string
	MaxKeyAge  time.Duration // default 90 days
	SkipACM    bool
	SkipIAM    bool
	SkipSecret bool
}

const defaultMaxKeyAge = 90 * 24 * time.Hour

func (s *AWSSource) Name() string { return "aws" }

func (s *AWSSource) Collect(ctx context.Context) ([]Item, error) {
	opts := []func(*config.LoadOptions) error{}
	if s.Region != "" {
		opts = append(opts, config.WithRegion(s.Region))
	}
	if s.Profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(s.Profile))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	account := ""
	if id, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{}); err == nil && id.Account != nil {
		account = *id.Account
	}

	items, per, err := collectServices(s.services(ctx, cfg, account))
	_ = per // the per-service breakdown is what `verify` reads; Collect wants the total
	return items, err
}

// awsService is one of the three, named so a failure can say which. Split out
// of Collect so the degradation rule — one denied permission must not lose the
// other two services' findings — is testable without an AWS account, which is
// the one property of this source nobody could check before.
type awsService struct {
	Name    string
	Skipped bool
	Collect func() ([]Item, error)
}

func (s *AWSSource) services(ctx context.Context, cfg aws.Config, account string) []awsService {
	return []awsService{
		{"acm", s.SkipACM, func() ([]Item, error) { return s.acm(ctx, cfg, account) }},
		{"iam", s.SkipIAM, func() ([]Item, error) { return s.iam(ctx, cfg, account) }},
		{"secretsmanager", s.SkipSecret, func() ([]Item, error) { return s.secrets(ctx, cfg, account) }},
	}
}

// serviceResult is what one service returned, for `verify`. A service that
// returned nothing is not the same as one that was denied, and not the same as
// one that was skipped — and a report that collapsed the three would let an
// account with no certificates read as an account whose ACM adapter works.
type serviceResult struct {
	Name    string
	Skipped bool
	Items   int
	Err     error
}

func collectServices(svcs []awsService) ([]Item, []serviceResult, error) {
	var items []Item
	var warnings []string
	results := make([]serviceResult, 0, len(svcs))
	for _, svc := range svcs {
		if svc.Skipped {
			results = append(results, serviceResult{Name: svc.Name, Skipped: true})
			continue
		}
		got, err := svc.Collect()
		if err != nil {
			// One denied permission must not lose the other two services'
			// findings. The partial items are still returned alongside the
			// error, so a caller that ignores the error is not silently
			// throwing away the two that worked.
			warnings = append(warnings, svc.Name+": "+err.Error())
			results = append(results, serviceResult{Name: svc.Name, Items: len(got), Err: err})
			items = append(items, got...)
			continue
		}
		results = append(results, serviceResult{Name: svc.Name, Items: len(got)})
		items = append(items, got...)
	}
	if len(warnings) > 0 {
		return items, results, fmt.Errorf("%s", strings.Join(warnings, "; "))
	}
	return items, results, nil
}

func (s *AWSSource) acm(ctx context.Context, cfg aws.Config, account string) ([]Item, error) {
	client := acm.NewFromConfig(cfg)
	var items []Item
	pager := acm.NewListCertificatesPaginator(client, &acm.ListCertificatesInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return items, err
		}
		for _, c := range page.CertificateSummaryList {
			if c.NotAfter == nil || c.CertificateArn == nil {
				continue // pending validation or imported without a parsed date
			}
			name := *c.CertificateArn
			if c.DomainName != nil {
				name = *c.DomainName
			}
			labels := map[string]string{"arn": *c.CertificateArn}
			if c.InUse != nil {
				// An unused certificate expiring is paperwork; one in use is an outage.
				labels["in-use"] = strconv.FormatBool(*c.InUse)
				labels[LabelPublic] = strconv.FormatBool(*c.InUse)
			}
			items = append(items, Item{
				Kind:      KindTLSCert,
				Name:      name,
				Expires:   *c.NotAfter,
				Source:    "aws:acm",
				Namespace: account,
				Labels:    labels,
			})
		}
	}
	return items, nil
}

func (s *AWSSource) iam(ctx context.Context, cfg aws.Config, account string) ([]Item, error) {
	maxAge := s.MaxKeyAge
	if maxAge == 0 {
		maxAge = defaultMaxKeyAge
	}
	client := iam.NewFromConfig(cfg)

	var items []Item
	users := iam.NewListUsersPaginator(client, &iam.ListUsersInput{})
	for users.HasMorePages() {
		page, err := users.NextPage(ctx)
		if err != nil {
			return items, err
		}
		for _, u := range page.Users {
			if u.UserName == nil {
				continue
			}
			keys := iam.NewListAccessKeysPaginator(client, &iam.ListAccessKeysInput{UserName: u.UserName})
			for keys.HasMorePages() {
				kp, err := keys.NextPage(ctx)
				if err != nil {
					return items, err
				}
				for _, k := range kp.AccessKeyMetadata {
					if k.CreateDate == nil || k.AccessKeyId == nil {
						continue
					}
					if string(k.Status) != "Active" {
						continue // an inactive key is already not working
					}
					items = append(items, Item{
						Kind:      KindIAMKey,
						Name:      *u.UserName + "/" + *k.AccessKeyId,
						Expires:   k.CreateDate.Add(maxAge), // rotation deadline, not an AWS expiry
						Source:    "aws:iam",
						Namespace: account,
						Labels: map[string]string{
							"created":     k.CreateDate.UTC().Format(time.RFC3339),
							"policy.days": strconv.Itoa(int(maxAge.Hours() / 24)),
						},
					})
				}
			}
		}
	}
	return items, nil
}

func (s *AWSSource) secrets(ctx context.Context, cfg aws.Config, account string) ([]Item, error) {
	client := secretsmanager.NewFromConfig(cfg)
	var items []Item
	pager := secretsmanager.NewListSecretsPaginator(client, &secretsmanager.ListSecretsInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return items, err
		}
		for _, sec := range page.SecretList {
			if sec.Name == nil || sec.NextRotationDate == nil {
				continue // no rotation schedule means no deadline to miss
			}
			items = append(items, Item{
				Kind:      KindSecret,
				Name:      *sec.Name,
				Expires:   *sec.NextRotationDate,
				Source:    "aws:secretsmanager",
				Namespace: account,
				Labels:    map[string]string{"rotation": "scheduled"},
			})
		}
	}
	return items, nil
}
