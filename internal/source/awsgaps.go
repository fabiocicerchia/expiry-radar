package source

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/acmpca"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/route53domains"
)

// The AWS services that expire and that ACM, IAM keys and Secrets Manager do
// not cover. Each is its own unit behind the same seam as the original three,
// so an account that denies one still reports the rest.
//
// RDS is the one AWS itself publishes a Prescriptive Guidance pattern for
// detecting, which is a fair signal of how often it bites: the regional CA
// bundle an instance trusts has a date, and when it passes the database stops
// accepting TLS connections from drivers that verify.

// rdsCertificates reports the regional CA certificates and, per instance, the
// one that instance is actually pinned to.
//
// Both matter and they are not the same question. The regional bundle says
// what is available; CertificateDetails says what each database will actually
// present, which is what breaks.
func (s *AWSSource) rdsCertificates(ctx context.Context, cfg aws.Config, account string) ([]Item, error) {
	client := rds.NewFromConfig(cfg)
	var items []Item

	certs := rds.NewDescribeCertificatesPaginator(client, &rds.DescribeCertificatesInput{})
	for certs.HasMorePages() {
		page, err := certs.NextPage(ctx)
		if err != nil {
			return items, err
		}
		for _, c := range page.Certificates {
			if c.ValidTill == nil || c.CertificateIdentifier == nil {
				continue
			}
			labels := map[string]string{}
			labels = label(labels, "certificate-type", aws.ToString(c.CertificateType))
			if c.CustomerOverrideValidTill != nil {
				labels = label(labels, "override-valid-till",
					c.CustomerOverrideValidTill.Format(time.RFC3339))
			}
			if !aws.ToBool(c.CustomerOverride) {
				// Not the account default: available, but nothing is pinned to
				// it, so its date is not a deadline anybody has to meet.
				labels[LabelInUse] = "false"
			}
			items = append(items, Item{
				Kind:      KindIntermediate,
				Name:      "rds-ca/" + *c.CertificateIdentifier,
				Expires:   *c.ValidTill,
				Source:    "aws:rds-ca",
				Namespace: account,
				Labels:    labels,
			})
		}
	}

	instances := rds.NewDescribeDBInstancesPaginator(client, &rds.DescribeDBInstancesInput{})
	for instances.HasMorePages() {
		page, err := instances.NextPage(ctx)
		if err != nil {
			return items, err
		}
		for _, db := range page.DBInstances {
			if db.CertificateDetails == nil || db.CertificateDetails.ValidTill == nil {
				continue
			}
			id := aws.ToString(db.DBInstanceIdentifier)
			if id == "" {
				continue
			}
			labels := map[string]string{}
			labels = label(labels, "ca", aws.ToString(db.CertificateDetails.CAIdentifier))
			labels = label(labels, "engine", aws.ToString(db.Engine))
			// A database reachable from outside the VPC is a different blast
			// radius from one that is not, and RDS states it outright.
			if aws.ToBool(db.PubliclyAccessible) {
				labels[LabelPublic] = "true"
			}
			items = append(items, Item{
				Kind:      KindTLSCert,
				Name:      "rds/" + id,
				Expires:   *db.CertificateDetails.ValidTill,
				Source:    "aws:rds",
				Namespace: account,
				Labels:    labels,
			})
		}
	}
	return items, nil
}

// privateCAs reports ACM Private CA authorities. These are trust anchors: an
// expired private CA invalidates every certificate it ever signed, all at once,
// and nothing behind it fails gracefully.
func (s *AWSSource) privateCAs(ctx context.Context, cfg aws.Config, account string) ([]Item, error) {
	client := acmpca.NewFromConfig(cfg)
	var items []Item

	pager := acmpca.NewListCertificateAuthoritiesPaginator(client,
		&acmpca.ListCertificateAuthoritiesInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return items, err
		}
		for _, ca := range page.CertificateAuthorities {
			if ca.NotAfter == nil || ca.Arn == nil {
				continue
			}
			name := aws.ToString(ca.Arn)
			if ca.CertificateAuthorityConfiguration != nil &&
				ca.CertificateAuthorityConfiguration.Subject != nil {
				if cn := aws.ToString(ca.CertificateAuthorityConfiguration.Subject.CommonName); cn != "" {
					name = cn
				}
			}
			labels := map[string]string{}
			labels = label(labels, "arn", aws.ToString(ca.Arn))
			labels = label(labels, "ca-type", string(ca.Type))
			labels = label(labels, "status", string(ca.Status))
			// A disabled or deleted CA is not signing anything.
			if ca.Status != "ACTIVE" {
				labels[LabelInUse] = "false"
			}
			items = append(items, Item{
				Kind:      KindTrustAnchor,
				Name:      "private-ca/" + name,
				Expires:   *ca.NotAfter,
				Source:    "aws:acm-pca",
				Namespace: account,
				Labels:    labels,
			})
		}
	}
	return items, nil
}

// iamCertificates reports the two IAM things with dates that the access-key
// adapter does not touch.
//
// Server certificates are the legacy ELB uploads — pre-ACM, uploaded once by
// somebody who has since left, and invisible in the console unless you go
// looking. SAML providers are worse: when one lapses, every federated login
// stops at once and it does not look like a certificate problem.
func (s *AWSSource) iamCertificates(ctx context.Context, cfg aws.Config, account string) ([]Item, error) {
	client := iam.NewFromConfig(cfg)
	var items []Item
	var warnings []string

	serverCerts := iam.NewListServerCertificatesPaginator(client, &iam.ListServerCertificatesInput{})
	for serverCerts.HasMorePages() {
		page, err := serverCerts.NextPage(ctx)
		if err != nil {
			warnings = append(warnings, "server certificates: "+err.Error())
			break
		}
		for _, c := range page.ServerCertificateMetadataList {
			if c.Expiration == nil || c.ServerCertificateName == nil {
				continue
			}
			items = append(items, Item{
				Kind:      KindTLSCert,
				Name:      "server-cert/" + *c.ServerCertificateName,
				Expires:   *c.Expiration,
				Source:    "aws:iam-server-cert",
				Namespace: account,
				Labels:    label(map[string]string{}, "arn", aws.ToString(c.Arn)),
			})
		}
	}

	providers, err := client.ListSAMLProviders(ctx, &iam.ListSAMLProvidersInput{})
	if err != nil {
		warnings = append(warnings, "saml providers: "+err.Error())
	} else {
		for _, p := range providers.SAMLProviderList {
			if p.ValidUntil == nil || p.Arn == nil {
				continue
			}
			name := *p.Arn
			if i := strings.LastIndex(name, "/"); i >= 0 && i+1 < len(name) {
				name = name[i+1:]
			}
			items = append(items, Item{
				Kind:      KindTrustAnchor,
				Name:      "saml/" + name,
				Expires:   *p.ValidUntil,
				Source:    "aws:iam-saml",
				Namespace: account,
				Labels:    label(map[string]string{}, "arn", *p.Arn),
			})
		}
	}

	return items, joinErrs(warnings)
}

// route53Domains reports registrations. Like every other registrar here, the
// value over the RDAP source is auto-renew: the date is the same, but only the
// registrar knows whether anybody is going to act on it.
func (s *AWSSource) route53Domains(ctx context.Context, cfg aws.Config) ([]Item, error) {
	// Route 53 Domains only exists in us-east-1. Without pinning it, the
	// endpoint rules resolve route53domains.<scan region>.amazonaws.com and
	// this unit fails on every account not scanning us-east-1 — which, since
	// it is on by default, would flip existing users from exit 0 to exit 3.
	cfg = cfg.Copy()
	cfg.Region = "us-east-1"
	client := route53domains.NewFromConfig(cfg)
	var items []Item

	pager := route53domains.NewListDomainsPaginator(client, &route53domains.ListDomainsInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return items, err
		}
		for _, d := range page.Domains {
			if d.Expiry == nil || d.DomainName == nil {
				continue
			}
			labels := map[string]string{LabelPublic: "true"}
			if aws.ToBool(d.AutoRenew) {
				labels[LabelRenewal] = RenewalManaged
			}
			if aws.ToBool(d.TransferLock) {
				labels = label(labels, "transfer-lock", strconv.FormatBool(true))
			}
			items = append(items, Item{
				Kind:      KindDomain,
				Name:      *d.DomainName,
				Expires:   *d.Expiry,
				Source:    "aws:route53domains",
				Namespace: *d.DomainName,
				Labels:    labels,
			})
		}
	}
	return items, nil
}
