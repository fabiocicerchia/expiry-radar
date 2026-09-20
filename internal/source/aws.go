package source

import (
	"context"
	"fmt"
	"strconv"
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
	// The services below need IAM permissions the first three do not, so each
	// has its own skip. They are on by default because they are read-only
	// Describe/List calls against services the account already pays for, and
	// the whole point of them is the things nobody remembered to look at.
	SkipRDS      bool
	SkipPCA      bool
	SkipIAMCerts bool
	SkipDomains  bool
}

const defaultMaxKeyAge = 90 * 24 * time.Hour

// Name identifies this source in an item's Source field and in -only.
func (s *AWSSource) Name() string { return "aws" }

// Collect reads ACM certificates, IAM access keys past the rotation age, and Secrets Manager entries.
//
// Read-only, like every source: expiry-radar never needs write access.
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
	if id, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx,
		&sts.GetCallerIdentityInput{}); err == nil && id.Account != nil {
		account = *id.Account
	}

	items, per, err := collectServices(s.services(ctx, cfg, account))
	_ = per // the per-service breakdown is what `verify` reads; Collect wants the total
	return items, err
}

// awsService is one of the three, named so a failure can say which. The
// collection loop itself is collectUnits in source.go — shared with the
// Kubernetes source so the degradation rule has one implementation, not two
// that can drift.
type awsService = collectUnit

// serviceResult is what one service returned, for `verify`.
type serviceResult = unitResult

func (s *AWSSource) services(ctx context.Context, cfg aws.Config, account string) []awsService {
	return []awsService{
		{"acm", s.SkipACM, func() ([]Item, error) { return s.acm(ctx, cfg, account) }},
		{"iam", s.SkipIAM, func() ([]Item, error) { return s.iam(ctx, cfg, account) }},
		{"secretsmanager", s.SkipSecret, func() ([]Item, error) { return s.secrets(ctx, cfg, account) }},
		{"rds", s.SkipRDS, func() ([]Item, error) { return s.rdsCertificates(ctx, cfg, account) }},
		{"acm-pca", s.SkipPCA, func() ([]Item, error) { return s.privateCAs(ctx, cfg, account) }},
		{"iam-certs", s.SkipIAMCerts, func() ([]Item, error) { return s.iamCertificates(ctx, cfg, account) }},
		{"route53domains", s.SkipDomains, func() ([]Item, error) { return s.route53Domains(ctx, cfg, account) }},
	}
}

func collectServices(svcs []awsService) ([]Item, []serviceResult, error) {
	return collectUnits(svcs)
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
			got, err := accessKeyItems(ctx, client, *u.UserName, account, maxAge)
			items = append(items, got...)
			if err != nil {
				return items, err
			}
		}
	}
	return items, nil
}

// accessKeyItems turns one user's active access keys into rotation deadlines.
// Split out of iam because walking users and reading one user's keys are two
// pages of AWS state, not one.
func accessKeyItems(
	ctx context.Context, client *iam.Client, user, account string, maxAge time.Duration,
) ([]Item, error) {
	var items []Item
	keys := iam.NewListAccessKeysPaginator(client, &iam.ListAccessKeysInput{UserName: &user})
	for keys.HasMorePages() {
		page, err := keys.NextPage(ctx)
		if err != nil {
			return items, err
		}
		for _, k := range page.AccessKeyMetadata {
			if k.CreateDate == nil || k.AccessKeyId == nil {
				continue
			}
			if string(k.Status) != "Active" {
				continue // an inactive key is already not working
			}
			items = append(items, Item{
				Kind:      KindIAMKey,
				Name:      user + "/" + *k.AccessKeyId,
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
