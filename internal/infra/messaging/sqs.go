package messaging

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type SQSConfig struct {
	EndpointURL string
	Region      string
	AccessKey   string
	SecretKey   string
	QueueURL    string
	DLQURL      string
}

func NewSQSClient(ctx context.Context, cfg SQSConfig) (*sqs.Client, error) {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.AccessKey == "" {
		cfg.AccessKey = "dummy"
	}
	if cfg.SecretKey == "" {
		cfg.SecretKey = "dummy"
	}

	customResolver := aws.EndpointResolverWithOptionsFunc(
		func(service, region string, options ...any) (aws.Endpoint, error) {
			if cfg.EndpointURL != "" {
				return aws.Endpoint{
					URL:               cfg.EndpointURL,
					SigningRegion:     cfg.Region,
					HostnameImmutable: true,
				}, nil
			}
			return aws.Endpoint{}, &aws.EndpointNotFoundError{}
		},
	)

	awsCfg, err := awsConfig.LoadDefaultConfig(
		ctx,
		awsConfig.WithRegion(cfg.Region),
		awsConfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
		awsConfig.WithEndpointResolverWithOptions(customResolver),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load aws config for sqs: %w", err)
	}

	return sqs.NewFromConfig(awsCfg), nil
}
