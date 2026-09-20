module github.com/fabiocicerchia/expiry-radar

go 1.24

// Minimum build toolchain: every govulncheck finding on this module is a
// stdlib CVE fixed by 1.26.6 (crypto/tls, encoding/asn1, encoding/xml,
// html/template, net/http, net/url).
toolchain go1.26.6

require (
	github.com/aws/aws-sdk-go-v2 v1.47.0
	github.com/aws/aws-sdk-go-v2/config v1.32.34
	github.com/aws/aws-sdk-go-v2/service/acm v1.43.3
	github.com/aws/aws-sdk-go-v2/service/acmpca v1.56.0
	github.com/aws/aws-sdk-go-v2/service/iam v1.57.1
	github.com/aws/aws-sdk-go-v2/service/rds v1.129.0
	github.com/aws/aws-sdk-go-v2/service/route53domains v1.44.0
	github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.44.3
	github.com/aws/aws-sdk-go-v2/service/sts v1.45.3
)

require (
	github.com/aws/aws-sdk-go-v2/credentials v1.19.33 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.34 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.3 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.3 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.35 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.19 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.5.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.33.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.38.3 // indirect
	github.com/aws/smithy-go v1.28.1 // indirect
)
