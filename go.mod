module github.com/fabiocicerchia/expiry-radar

go 1.26

// Minimum build toolchain: every govulncheck finding on this module is a
// stdlib CVE fixed by 1.26.11 (crypto/tls, encoding/asn1, encoding/xml,
// html/template, net/http, net/url).
toolchain go1.26.11

require (
	github.com/aws/aws-sdk-go-v2 v1.48.0
	github.com/aws/aws-sdk-go-v2/config v1.33.18
	github.com/aws/aws-sdk-go-v2/service/acm v1.51.5
	github.com/aws/aws-sdk-go-v2/service/acmpca v1.56.12
	github.com/aws/aws-sdk-go-v2/service/iam v1.65.0
	github.com/aws/aws-sdk-go-v2/service/rds v1.133.0
	github.com/aws/aws-sdk-go-v2/service/route53domains v1.46.2
	github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.51.0
	github.com/aws/aws-sdk-go-v2/service/sts v1.52.0
)

require (
	github.com/aws/aws-sdk-go-v2/credentials v1.21.0 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.20.9 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.12 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.12 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.12 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.25 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.12 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.13.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.39.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.44.5 // indirect
	github.com/aws/smithy-go v1.28.9 // indirect
)
