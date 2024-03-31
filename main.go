package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/tidwall/gjson"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/urfave/cli/v2"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	// The sts GetCallerIdentity request is valid for 15 minutes regardless of this parameters value after it has been
	// signed, but we set this unused parameter to 60 for legacy reasons (we check for a value between 0 and 60 on the
	// server side in 0.3.0 or earlier).  IT IS IGNORED.  If we can get STS to support x-amz-expires, then we should
	// set this parameter to the actual expiration, and make it configurable.
	requestPresignParam = "60"
	// The actual token expiration (presigned STS urls are valid for 15 minutes after timestamp in x-amz-date).
	presignedURLExpiration = 15 * time.Minute
	v1Prefix               = "k8s-aws-v1."
	clusterIDHeader        = "x-k8s-aws-id"
	// Format of the X-Amz-Date header used for expiration
	// https://golang.org/pkg/time/#pkg-constants
	dateHeaderFormat = "20060102T150405Z"
)

var (
	awsConfig    *aws.Config
	awsConfigErr error

	logger           *zap.Logger
	logSugar         *zap.SugaredLogger
	version, reqUuid string

	qbconfKubeconfigEnvVarName = "QBCONF_KUBECONFIG"
)

func init() {

	reqUuid = uuid.New().String()

	logger, _ = zap.NewProduction(zap.Fields(zap.String("request_uuid", reqUuid)))
	defer logger.Sync() // flushes buffer, if any
	logSugar = logger.Sugar()
}

func main() {

	app := cli.NewApp()

	app.Version = version
	app.Authors = []*cli.Author{
		{
			Name:  "NinjaOps by https://raftech.nl",
			Email: "hello@raftech.nl",
		},
	}
	app.Name = "qbconf"
	app.Usage = "Minimalistic Kubernetes kubeconfig file generator using AWS STS and EKS APIs"

	app.Commands = []*cli.Command{
		{
			Name:  "generate",
			Usage: "Generate a kubeconfig file for a kubernetes cluster",
			Subcommands: []*cli.Command{
				{
					Name:  "aws",
					Usage: "Generate a kubeconfig file for an EKS cluster",
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name:     "role-arn",
							Usage:    "ARN of the AWS IAM role to assume",
							EnvVars:  []string{"AWS_ROLE_ARN"},
							Value:    "",
							Required: false,
						},
						&cli.StringFlag{
							Name:     "region",
							Usage:    "AWS region",
							EnvVars:  []string{"AWS_REGION"},
							Value:    "eu-west-1",
							Required: false,
						},
						&cli.StringFlag{
							Name:     "role-session-name",
							Usage:    "Name of the AWS STS role session to create",
							EnvVars:  []string{"AWS_ROLE_SESSION_NAME"},
							Value:    "qbconf-session",
							Required: false,
						},
						&cli.StringFlag{
							Name:     "cluster-name",
							Usage:    "Name of the EKS cluster to generate a kubeconfig",
							Required: true,
						},
						&cli.StringFlag{
							Name:     "output-file",
							Usage:    "Name of the file to write the generated kubeconfig to",
							Value:    "kubeconfig.yaml",
							Required: false,
						},
						&cli.BoolFlag{
							Name:  "with-assume-role",
							Usage: "Enables assuming of IAM role via STS",
							Value: false,
						},
						&cli.BoolFlag{
							Name:  "with-gha-oidc",
							Usage: "Enables assuming of IAM role via OIDC for Github Actions",
							Value: false,
						},
						&cli.BoolFlag{
							Name:  "with-gitlab-oidc",
							Usage: "Enables assuming of IAM role via OIDC for Gitlab CI/CD",
							Value: false,
						},
					},
					Before: func(c *cli.Context) error {
						awsConfigErr = error(nil)

						awsConfig, awsConfigErr = loadAWSConfig(c.String("region"))

						if awsConfigErr != nil {
							logSugar.Error(awsConfigErr)
							return awsConfigErr
						}

						logSugar.Debug("loaded default AWS config successfully")
						return awsConfigErr
					},
					Action: func(c *cli.Context) error {

						qbconfOperationMode := "generate::aws::with-default-credentials"
						logSugar.Infow("set default operating mode",
							"mode", qbconfOperationMode,
						)

						if c.Bool("with-assume-role") {
							qbconfOperationMode = "generate::aws::with-assume-role"

							logSugar.Infow("change operating mode",
								"mode", qbconfOperationMode,
							)

							provider := assumeRoleByArn(c.String("role-arn"), c.String("role-session-name"), awsConfig)
							awsConfig.Credentials = provider
						}
						if c.Bool("with-gha-oidc") || c.Bool("with-gitlab-oidc") {
							qbconfOperationMode = "generate::aws::use-oidc"

							logSugar.Infow("change operating mode",
								"mode", qbconfOperationMode,
							)

							// Add your own configuration here
							maxRetries := 3
							backoffBaseSeconds := 2

							assumeRoleWithWebIdentityErr := retryWithExponentialBackoff(maxRetries, backoffBaseSeconds, func() error {
								var err error
								var oidcToken *string

								if c.Bool("with-gha-oidc") {
									oidcToken, err = getOidcGithubActionsToken()
								} else {
									oidcToken, err = getOidcGitlabToken()
								}

								if err != nil {
									return err
								}

								awsConfig.Credentials, err = assumeRoleWithWebIdentity(c.String("role-arn"), c.String("role-session-name"), *oidcToken, awsConfig)
								return err
							})

							if assumeRoleWithWebIdentityErr != nil {
								logSugar.Error(assumeRoleWithWebIdentityErr)
								return assumeRoleWithWebIdentityErr
							}

							// OidcToken, OidcTokenErr := getOidcGithubActionsToken()
							// if OidcTokenErr != nil {
							// 	logSugar.Error(OidcTokenErr)
							// 	return OidcTokenErr
							// }

							// awsConfig.Credentials = assumeRoleWithWebIdentity(c.String("role-arn"), c.String("role-session-name"), *OidcToken, awsConfig)
						}

						_, getAWSIdentityErr := getAWSIdentity(*awsConfig)
						if getAWSIdentityErr != nil {
							logSugar.Error(getAWSIdentityErr)
							return getAWSIdentityErr
						}

						kubeconfigByteArr, errKubeConfig := generateKubeconfigEKS(c.String("region"), c.String("cluster-name"))
						if errKubeConfig != nil {
							logSugar.Error(errKubeConfig)
							return errKubeConfig
						}

						logSugar.Infow("writing kubeconfig to file", "file", c.String("output-file"))
						writeToFile(c.String("output-file"), kubeconfigByteArr)

						return nil
					},
				},
			},
			Action: func(c *cli.Context) error {
				cli.ShowSubcommandHelp(c)
				return nil
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

// Loads the default AWS configuration - accordingly to the SDK documentation of resolving credentials
func loadAWSConfig(region string) (*aws.Config, error) {

	logSugar.Info("Loading default AWS config...")

	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		logSugar.Errorw("failed to load SDK config", err)
		return nil, err
	}

	return &cfg, nil
}

// Gets the current identity which we have from AWS
func getAWSIdentity(cfg aws.Config) (*sts.GetCallerIdentityOutput, error) {

	logSugar.Info("Getting AWS identity... (getAWSIdentity)")

	svc := sts.NewFromConfig(cfg)

	input := &sts.GetCallerIdentityInput{}
	result, err := svc.GetCallerIdentity(context.TODO(), input)
	if err != nil {
		var aerr awserr.Error
		if ok := errors.As(err, &aerr); ok {
			switch aerr.Code() {
			default:
				return nil, fmt.Errorf("aws error: %s", aerr.Error())
			}
		} else {
			return nil, fmt.Errorf("unexpected error: %s", err.Error())
		}
	}

	logSugar.Infow("retrieved caller identity from AWS",
		// Structured context as loosely typed key-value pairs.
		"arn", *result.Arn,
		"account", *result.Account,
	)

	return result, nil
}

// Function to assume a role by ARN provided
func assumeRoleByArn(roleArn, roleSessionName string, awsConfig *aws.Config) *stscreds.AssumeRoleProvider {

	// Create an STS client using the default config
	stsClient := sts.NewFromConfig(*awsConfig)

	// Create an AssumeRoleProvider that will assume the specified role
	roleProvider := stscreds.NewAssumeRoleProvider(stsClient, roleArn, func(o *stscreds.AssumeRoleOptions) {
		o.RoleSessionName = roleSessionName
	})

	return roleProvider
}

// Function to assume role with OIDC ( token )
func assumeRoleWithWebIdentity(roleArn, roleSessionName, token string, awsConfig *aws.Config) (*aws.CredentialsCache, error) {

	// Create an STS client using the default config
	stsClient := sts.NewFromConfig(*awsConfig)

	// Set up the AssumeRoleWithWebIdentity input
	input := &sts.AssumeRoleWithWebIdentityInput{
		RoleArn:          aws.String(roleArn),
		RoleSessionName:  aws.String(roleSessionName),
		WebIdentityToken: aws.String(token),
	}

	// Call the AssumeRoleWithWebIdentity API to assume the IAM role
	resp, err := stsClient.AssumeRoleWithWebIdentity(context.Background(), input)
	if err != nil {
		return nil, err
	}

	// value := aws.Credentials{
	// 	AccessKeyID:     aws.ToString(resp.Credentials.AccessKeyId),
	// 	SecretAccessKey: aws.ToString(resp.Credentials.SecretAccessKey),
	// 	SessionToken:    aws.ToString(resp.Credentials.SessionToken),
	// 	Source:          "Github",
	// 	CanExpire:       true,
	// 	Expires:         *resp.Credentials.Expiration,
	// }

	credsProvider := aws.NewCredentialsCache(
		credentials.NewStaticCredentialsProvider(
			*resp.Credentials.AccessKeyId,
			*resp.Credentials.SecretAccessKey,
			*resp.Credentials.SessionToken,
		),
	)

	return credsProvider, nil
}

// Function to generate a kubeconfig for a given EKS cluster
func generateKubeconfigEKS(region, eksClusterName string) ([]byte, error) {

	stsSvc := sts.NewFromConfig(*awsConfig)

	logSugar.Info("generating NewPresignClient ...")
	presignClient := sts.NewPresignClient(stsSvc, sts.WithPresignClientFromClientOptions(func(o *sts.Options) {
		o.Credentials = awsConfig.Credentials
	}))

	logSugar.Info("calling PresignGetCallerIdentity ...")
	getCallerIdentity, err := presignClient.PresignGetCallerIdentity(context.Background(), &sts.GetCallerIdentityInput{}, func(presignOptions *sts.PresignOptions) {
		presignOptions.ClientOptions = append(presignOptions.ClientOptions, func(stsOptions *sts.Options) {
			// Add clusterId Header
			stsOptions.APIOptions = append(stsOptions.APIOptions, smithyhttp.SetHeaderValue(clusterIDHeader, eksClusterName))
			// Add back useless X-Amz-Expires query param
			stsOptions.APIOptions = append(stsOptions.APIOptions, smithyhttp.SetHeaderValue("X-Amz-Expires", requestPresignParam))
		})
	})

	if err != nil {
		return nil, err
	}

	logSugar.Info("cretaing new EKS client...")
	eksSvc := eks.NewFromConfig(*awsConfig)

	logSugar.Info("describing EKS cluster...")
	res, err := eksSvc.DescribeCluster(context.TODO(), &eks.DescribeClusterInput{
		Name: aws.String(eksClusterName),
	})
	if err != nil {
		return nil, err
	}

	logSugar.Info("decoding certificateAuthorityData...")
	certificateAuthorityData, _ := base64.StdEncoding.DecodeString(*res.Cluster.CertificateAuthority.Data)

	logSugar.Info("generating kubeconfig for the EKS cluster ...")
	config := &api.Config{
		Clusters: map[string]*api.Cluster{
			*res.Cluster.Name: {
				Server:                   *res.Cluster.Endpoint,
				CertificateAuthorityData: []byte(certificateAuthorityData),
			},
		},
		Contexts: map[string]*api.Context{
			*res.Cluster.Name: {
				Cluster:   *res.Cluster.Name,
				Namespace: "default",
				AuthInfo:  *res.Cluster.Name,
			},
		},
		AuthInfos: map[string]*api.AuthInfo{
			*res.Cluster.Name: {
				Token: v1Prefix + base64.RawURLEncoding.EncodeToString([]byte(getCallerIdentity.URL)),
			},
		},
		CurrentContext: *res.Cluster.Name,
	}

	logSugar.Info("output kubeconfig byte[]")
	configBytes, err := clientcmd.Write(*config)
	if err != nil {
		return nil, err
	}

	return configBytes, nil
}

func maskString(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:4] + strings.Repeat("*", len(s)-8) + s[len(s)-4:]
}

func writeToFile(outputPath string, configBytes []byte) error {

	err := ioutil.WriteFile(outputPath, configBytes, 0644)
	if err != nil {
		panic(err)
	}

	return nil
}

// MissingEnvVarError is a custom error type for missing environment variables.
type MissingEnvVarError struct {
	EnvVarName string
}

// Error implements the error interface for MissingEnvVarError.
func (e MissingEnvVarError) Error() string {
	return fmt.Sprintf("missing required environment variable: %s", e.EnvVarName)
}

func getOidcGitlabToken() (*string, error) {
	token, exists := os.LookupEnv("CI_JOB_JWT_V2")
	if !exists {
		err := MissingEnvVarError{EnvVarName: "CI_JOB_JWT_V2"}
		logSugar.Error(err)
		return nil, fmt.Errorf("CI_JOB_JWT_V2 environment variable is not available")
	}
	return &token, nil
}

func getOidcGithubActionsToken() (*string, error) {

	// These environment variables are required for this action to run.
	// They will be available only if the workflow calling the action/CLI will have
	// write permissions to the id-token
	requiredEnvVars := []string{"ACTIONS_ID_TOKEN_REQUEST_URL", "ACTIONS_ID_TOKEN_REQUEST_TOKEN"}

	// Check for missing environment variables and return an error if any are missing.
	for _, envVar := range requiredEnvVars {
		if _, exists := os.LookupEnv(envVar); !exists {
			err := MissingEnvVarError{EnvVarName: envVar}
			logSugar.Error(err)
			os.Exit(1)
		}
	}

	logSugar.Debug("creating new resty client instance...")

	client := resty.New()
	client.
		// Set retry count to non zero to enable retries
		SetRetryCount(3).
		// override initial retry wait time.
		SetRetryWaitTime(2 * time.Second).
		// MaxWaitTime can be overridden as well.
		SetRetryMaxWaitTime(10 * time.Second)

	logSugar.Info("created new resty client")

	logSugar.Info("retrieve env vars for requesting token value towards OIDC endpoint")

	// ACTIONS_ID_TOKEN_REQUEST_URL
	logSugar.Debug("retrieval of ACTIONS_ID_TOKEN_REQUEST_URL env variable")
	tokenRequestURL := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	logSugar.Infow("retrieved ACTIONS_ID_TOKEN_REQUEST_URL",
		"oidc_token_request_url", tokenRequestURL,
	)

	//ACTIONS_ID_TOKEN_REQUEST_TOKEN
	logSugar.Debug("retrieval of ACTIONS_ID_TOKEN_REQUEST_TOKEN env variable")
	tokenRequestToken := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	logSugar.Info("retrieved ACTIONS_ID_TOKEN_REQUEST_TOKEN")

	logSugar.Debugw("prepared URL for requesting token value towards OIDC endpoint",
		"oidc_token_request_url", "%s&audience=sts.amazonaws.com",
	)
	resp, err := client.R().
		SetAuthToken(tokenRequestToken).
		Get(fmt.Sprintf("%s&audience=sts.amazonaws.com", tokenRequestURL))

	if err != nil {
		logSugar.Error("failed to retrieve token value from OIDC endpoint", err)
		return nil, err
	}

	tokenValue := gjson.Get(resp.String(), "value").String()

	return &tokenValue, nil
}

func retryWithExponentialBackoff(maxRetries int, backoffBaseSeconds int, operation func() error) error {
	var err error
	for i := 0; i < maxRetries; i++ {
		err = operation()
		if err == nil {
			return nil
		}

		waitTime := time.Duration(math.Pow(float64(backoffBaseSeconds), float64(i))) * time.Second
		logSugar.Infow("retry wait added", "wait_time", waitTime, "attempt", i, "max_retries", maxRetries)
		time.Sleep(waitTime)
	}

	return err
}
