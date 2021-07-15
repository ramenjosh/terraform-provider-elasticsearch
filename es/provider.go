package es

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	awscredentials "github.com/aws/aws-sdk-go/aws/credentials"
	awsstscreds "github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	awssession "github.com/aws/aws-sdk-go/aws/session"
	awssigv4 "github.com/aws/aws-sdk-go/aws/signer/v4"
	awssts "github.com/aws/aws-sdk-go/service/sts"
	"github.com/deoxxa/aws_signing_client"

	"github.com/hashicorp/terraform-plugin-sdk/helper/pathorcontents"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"
	elastic7 "github.com/olivere/elastic/v7"

	elastic5 "gopkg.in/olivere/elastic.v5"
	elastic6 "gopkg.in/olivere/elastic.v6"
)

var awsUrlRegexp = regexp.MustCompile(`([a-z0-9-]+).es.amazonaws.com$`)

type ProviderConf struct {
	rawUrl             string
	insecure           bool
	sniffing           bool
	healthchecking     bool
	cacertFile         string
	username           string
	password           string
	token              string
	tokenName          string
	parsedUrl          *url.URL
	signAWSRequests    bool
	esVersion          string
	awsRegion          string
	awsAssumeRoleArn   string
	awsAccessKeyId     string
	awsSecretAccessKey string
	awsSessionToken    string
	awsProfile         string
	certPemPath        string
	keyPemPath         string
	kibanaUrl          string
	hostOverride       string
}

func Provider() terraform.ResourceProvider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"url": {
				Type:        schema.TypeString,
				Required:    true,
				DefaultFunc: schema.EnvDefaultFunc("ELASTICSEARCH_URL", nil),
				Description: "Elasticsearch URL",
			},
			"kibana_url": {
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("KIBANA_URL", nil),
				Description: "URL to reach the Kibana API",
			},
			"sniff": {
				Type:        schema.TypeBool,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("ELASTICSEARCH_SNIFF", true),
				Description: "Set the node sniffing option for the elastic client. Client won't work with sniffing if nodes are not routable.",
			},
			"healthcheck": {
				Type:        schema.TypeBool,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("ELASTICSEARCH_HEALTH", true),
				Description: "Set the client healthcheck option for the elastic client. Healthchecking is designed for direct access to the cluster.",
			},
			"username": {
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("ELASTICSEARCH_USERNAME", nil),
				Description: "Username to use to connect to elasticsearch using basic auth",
			},
			"password": {
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("ELASTICSEARCH_PASSWORD", nil),
				Description: "Password to use to connect to elasticsearch using basic auth",
			},
			"token": {
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("ELASTICSEARCH_TOKEN", nil),
				Description: "A bearer token or ApiKey for an Authorization header, e.g. Active Directory API key.",
			},
			"token_name": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "ApiKey",
				Description: "The type of token, usually ApiKey or Bearer",
			},
			"aws_assume_role_arn": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "Amazon Resource Name of an IAM Role to assume prior to making AWS API calls.",
			},
			"aws_access_key": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "The access key for use with AWS Elasticsearch Service domains",
			},
			"aws_secret_key": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "The secret key for use with AWS Elasticsearch Service domains",
			},
			"aws_token": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "The session token for use with AWS Elasticsearch Service domains",
			},
			"aws_profile": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "The AWS profile for use with AWS Elasticsearch Service domains",
			},
			"aws_region": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "The AWS region for use in signing of AWS elasticsearch requests. Must be specified in order to use AWS URL signing with AWS ElasticSearch endpoint exposed on a custom DNS domain.",
			},
			"cacert_file": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "A Custom CA certificate",
			},
			"insecure": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Disable SSL verification of API calls",
			},
			"client_cert_path": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "A X509 certificate to connect to elasticsearch",
				DefaultFunc: schema.EnvDefaultFunc("ES_CLIENT_CERTIFICATE_PATH", ""),
			},
			"client_key_path": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "A X509 key to connect to elasticsearch",
				DefaultFunc: schema.EnvDefaultFunc("ES_CLIENT_KEY_PATH", ""),
			},
			"sign_aws_requests": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "Enable signing of AWS elasticsearch requests. The `url` must refer to AWS ES domain (`*.<region>.es.amazonaws.com`), or `aws_region` must be specified explicitly.",
			},
			"elasticsearch_version": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "ElasticSearch Version",
			},
			"host_override": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "Override for the 'Host'. Useful when proxying requests via a tunnel.",
			},
		},

		ResourcesMap: map[string]*schema.Resource{
			"elasticsearch_destination":                     resourceElasticsearchDeprecatedDestination(),
			"elasticsearch_index":                           resourceElasticsearchIndex(),
			"elasticsearch_index_lifecycle_policy":          resourceElasticsearchDeprecatedIndexLifecyclePolicy(),
			"elasticsearch_index_template":                  resourceElasticsearchIndexTemplate(),
			"elasticsearch_composable_index_template":       resourceElasticsearchComposableIndexTemplate(),
			"elasticsearch_component_template":              resourceElasticsearchComponentTemplate(),
			"elasticsearch_ingest_pipeline":                 resourceElasticsearchIngestPipeline(),
			"elasticsearch_kibana_alert":                    resourceElasticsearchKibanaAlert(),
			"elasticsearch_kibana_object":                   resourceElasticsearchKibanaObject(),
			"elasticsearch_monitor":                         resourceElasticsearchDeprecatedMonitor(),
			"elasticsearch_snapshot_repository":             resourceElasticsearchSnapshotRepository(),
			"elasticsearch_watch":                           resourceElasticsearchDeprecatedWatch(),
			"elasticsearch_opendistro_destination":          resourceElasticsearchOpenDistroDestination(),
			"elasticsearch_opendistro_ism_policy":           resourceElasticsearchOpenDistroISMPolicy(),
			"elasticsearch_opendistro_ism_policy_mapping":   resourceElasticsearchOpenDistroISMPolicyMapping(),
			"elasticsearch_opendistro_monitor":              resourceElasticsearchOpenDistroMonitor(),
			"elasticsearch_opendistro_roles_mapping":        resourceElasticsearchOpenDistroRolesMapping(),
			"elasticsearch_opendistro_role":                 resourceElasticsearchOpenDistroRole(),
			"elasticsearch_opendistro_user":                 resourceElasticsearchOpenDistroUser(),
			"elasticsearch_opendistro_kibana_tenant":        resourceElasticsearchOpenDistroKibanaTenant(),
			"elasticsearch_xpack_index_lifecycle_policy":    resourceElasticsearchXpackIndexLifecyclePolicy(),
			"elasticsearch_xpack_license":                   resourceElasticsearchXpackLicense(),
			"elasticsearch_xpack_role":                      resourceElasticsearchXpackRole(),
			"elasticsearch_xpack_role_mapping":              resourceElasticsearchXpackRoleMapping(),
			"elasticsearch_xpack_snapshot_lifecycle_policy": resourceElasticsearchXpackSnapshotLifecyclePolicy(),
			"elasticsearch_xpack_user":                      resourceElasticsearchXpackUser(),
			"elasticsearch_xpack_watch":                     resourceElasticsearchXpackWatch(),
		},

		DataSourcesMap: map[string]*schema.Resource{
			"elasticsearch_destination":            dataSourceElasticsearchDeprecatedDestination(),
			"elasticsearch_host":                   dataSourceElasticsearchHost(),
			"elasticsearch_opendistro_destination": dataSourceElasticsearchOpenDistroDestination(),
		},

		ConfigureFunc: providerConfigure,
	}
}

func providerConfigure(d *schema.ResourceData) (interface{}, error) {
	rawUrl := d.Get("url").(string)
	parsedUrl, err := url.Parse(rawUrl)
	if err != nil {
		return nil, err
	}

	return &ProviderConf{
		rawUrl:          rawUrl,
		kibanaUrl:       d.Get("kibana_url").(string),
		insecure:        d.Get("insecure").(bool),
		sniffing:        d.Get("sniff").(bool),
		healthchecking:  d.Get("healthcheck").(bool),
		cacertFile:      d.Get("cacert_file").(string),
		username:        d.Get("username").(string),
		password:        d.Get("password").(string),
		token:           d.Get("token").(string),
		tokenName:       d.Get("token_name").(string),
		parsedUrl:       parsedUrl,
		signAWSRequests: d.Get("sign_aws_requests").(bool),
		esVersion:       d.Get("elasticsearch_version").(string),
		awsRegion:       d.Get("aws_region").(string),

		awsAssumeRoleArn:   d.Get("aws_assume_role_arn").(string),
		awsAccessKeyId:     d.Get("aws_access_key").(string),
		awsSecretAccessKey: d.Get("aws_secret_key").(string),
		awsSessionToken:    d.Get("aws_token").(string),
		awsProfile:         d.Get("aws_profile").(string),
		certPemPath:        d.Get("client_cert_path").(string),
		keyPemPath:         d.Get("client_key_path").(string),
		hostOverride:       d.Get("host_override").(string),
	}, nil
}

func getClient(conf *ProviderConf) (interface{}, error) {
	opts := []elastic7.ClientOptionFunc{
		elastic7.SetURL(conf.rawUrl),
		elastic7.SetScheme(conf.parsedUrl.Scheme),
		elastic7.SetSniff(conf.sniffing),
		elastic7.SetHealthcheck(conf.healthchecking),
	}

	if conf.parsedUrl.User.Username() != "" {
		p, _ := conf.parsedUrl.User.Password()
		opts = append(opts, elastic7.SetBasicAuth(conf.parsedUrl.User.Username(), p))
	}
	if conf.username != "" && conf.password != "" {
		opts = append(opts, elastic7.SetBasicAuth(conf.username, conf.password))
	}

	if m := awsUrlRegexp.FindStringSubmatch(conf.parsedUrl.Hostname()); m != nil && conf.signAWSRequests {
		log.Printf("[INFO] Using AWS: %+v", m[1])
		opts = append(opts, elastic7.SetHttpClient(awsHttpClient(m[1], conf, map[string]string{})), elastic7.SetSniff(false))
	} else if awsRegion := conf.awsRegion; conf.awsRegion != "" && conf.signAWSRequests {
		log.Printf("[INFO] Using AWS: %+v", awsRegion)
		opts = append(opts, elastic7.SetHttpClient(awsHttpClient(awsRegion, conf, map[string]string{})), elastic7.SetSniff(false))
	} else if conf.insecure || conf.cacertFile != "" {
		opts = append(opts, elastic7.SetHttpClient(tlsHttpClient(conf, map[string]string{})), elastic7.SetSniff(false))
	} else if conf.token != "" {
		opts = append(opts, elastic7.SetHttpClient(tokenHttpClient(conf.token, conf.tokenName, conf.insecure, map[string]string{})), elastic7.SetSniff(false))
	}

	var relevantClient interface{}
	client, err := elastic7.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	log.Printf("[INFO] client: %+v", client)
	relevantClient = client

	// Use the v7 client to ping the cluster to determine the version if one was not provided
	if conf.esVersion == "" {
		log.Printf("[INFO] Pinging url to determine version %+v", conf.rawUrl)
		info, _, err := client.Ping(conf.rawUrl).Do(context.TODO())
		if err != nil {
			return nil, err
		}
		conf.esVersion = info.Version.Number
	}

	if conf.esVersion < "7.0.0" && conf.esVersion >= "6.0.0" {
		log.Printf("[INFO] Using ES 6")
		opts := []elastic6.ClientOptionFunc{
			elastic6.SetURL(conf.rawUrl),
			elastic6.SetScheme(conf.parsedUrl.Scheme),
			elastic6.SetSniff(conf.sniffing),
			elastic6.SetHealthcheck(conf.healthchecking),
		}

		if conf.parsedUrl.User.Username() != "" {
			p, _ := conf.parsedUrl.User.Password()
			opts = append(opts, elastic6.SetBasicAuth(conf.parsedUrl.User.Username(), p))
		}
		if conf.username != "" && conf.password != "" {
			opts = append(opts, elastic6.SetBasicAuth(conf.username, conf.password))
		}

		if m := awsUrlRegexp.FindStringSubmatch(conf.parsedUrl.Hostname()); m != nil && conf.signAWSRequests {
			log.Printf("[INFO] Using AWS: %+v", m[1])
			opts = append(opts, elastic6.SetHttpClient(awsHttpClient(m[1], conf, map[string]string{})), elastic6.SetSniff(false))
		} else if awsRegion := conf.awsRegion; conf.awsRegion != "" && conf.signAWSRequests {
			log.Printf("[INFO] Using AWS: %+v", conf.awsRegion)
			opts = append(opts, elastic6.SetHttpClient(awsHttpClient(awsRegion, conf, map[string]string{})), elastic6.SetSniff(false))
		} else if conf.insecure || conf.cacertFile != "" {
			opts = append(opts, elastic6.SetHttpClient(tlsHttpClient(conf, map[string]string{})), elastic6.SetSniff(false))
		} else if conf.token != "" {
			opts = append(opts, elastic6.SetHttpClient(tokenHttpClient(conf.token, conf.tokenName, conf.insecure, map[string]string{})), elastic6.SetSniff(false))
		}

		relevantClient, err = elastic6.NewClient(opts...)
		if err != nil {
			return nil, err
		}
	} else if conf.esVersion < "6.0.0" && conf.esVersion >= "5.0.0" {
		log.Printf("[INFO] Using ES 5")
		opts := []elastic5.ClientOptionFunc{
			elastic5.SetURL(conf.rawUrl),
			elastic5.SetScheme(conf.parsedUrl.Scheme),
			elastic5.SetSniff(conf.sniffing),
			elastic5.SetHealthcheck(conf.healthchecking),
		}

		if conf.parsedUrl.User.Username() != "" {
			p, _ := conf.parsedUrl.User.Password()
			opts = append(opts, elastic5.SetBasicAuth(conf.parsedUrl.User.Username(), p))
		}
		if conf.username != "" && conf.password != "" {
			opts = append(opts, elastic5.SetBasicAuth(conf.username, conf.password))
		}

		if m := awsUrlRegexp.FindStringSubmatch(conf.parsedUrl.Hostname()); m != nil && conf.signAWSRequests {
			opts = append(opts, elastic5.SetHttpClient(awsHttpClient(m[1], conf, map[string]string{})), elastic5.SetSniff(false))
		} else if awsRegion := conf.awsRegion; conf.awsRegion != "" && conf.signAWSRequests {
			log.Printf("[INFO] Using AWS: %+v", conf.awsRegion)
			opts = append(opts, elastic5.SetHttpClient(awsHttpClient(awsRegion, conf, map[string]string{})), elastic5.SetSniff(false))
		} else if conf.insecure || conf.cacertFile != "" {
			opts = append(opts, elastic5.SetHttpClient(tlsHttpClient(conf, map[string]string{})), elastic5.SetSniff(false))
		} else if conf.token != "" {
			opts = append(opts, elastic5.SetHttpClient(tokenHttpClient(conf.token, conf.tokenName, conf.insecure, map[string]string{})), elastic5.SetSniff(false))
		}

		relevantClient, err = elastic5.NewClient(opts...)
		if err != nil {
			return nil, err
		}
	} else if conf.esVersion < "5.0.0" {
		return nil, errors.New("ElasticSearch is older than 5.0.0!")
	}

	return relevantClient, nil
}

func getKibanaClient(conf *ProviderConf) (interface{}, error) {
	// use either the provided version of elasticsearch or the version of
	// elasticsearch determined by pinging the cluster. Base AWS or other auth
	// off of the same ES config
	esClient, err := getClient(conf)
	if err != nil {
		return nil, err
	}

	switch esClient.(type) {
	case *elastic7.Client:
		opts := []elastic7.ClientOptionFunc{
			elastic7.SetURL(conf.kibanaUrl),
			elastic7.SetScheme(conf.parsedUrl.Scheme),
			// kibana api does not support sniff/health check
			elastic7.SetSniff(false),
			elastic7.SetHealthcheck(false),
		}

		if conf.parsedUrl.User.Username() != "" {
			p, _ := conf.parsedUrl.User.Password()
			opts = append(opts, elastic7.SetBasicAuth(conf.parsedUrl.User.Username(), p))
		}
		if conf.username != "" && conf.password != "" {
			opts = append(opts, elastic7.SetBasicAuth(conf.username, conf.password))
		}

		headers := map[string]string{"kbn-xsrf": "true"}

		if m := awsUrlRegexp.FindStringSubmatch(conf.parsedUrl.Hostname()); m != nil && conf.signAWSRequests {
			log.Printf("[INFO] Using AWS: %+v", m[1])
			opts = append(opts, elastic7.SetHttpClient(awsHttpClient(m[1], conf, headers)), elastic7.SetSniff(false))
		} else if awsRegion := conf.awsRegion; conf.awsRegion != "" && conf.signAWSRequests {
			log.Printf("[INFO] Using AWS: %+v", awsRegion)
			opts = append(opts, elastic7.SetHttpClient(awsHttpClient(awsRegion, conf, headers)), elastic7.SetSniff(false))
		} else if conf.insecure || conf.cacertFile != "" {
			opts = append(opts, elastic7.SetHttpClient(tlsHttpClient(conf, headers)))
		} else if conf.token != "" {
			opts = append(opts, elastic7.SetHttpClient(tokenHttpClient(conf.token, conf.tokenName, conf.insecure, headers)), elastic7.SetSniff(false))
		} else {
			client := http.DefaultClient
			rt := WithHeader(client.Transport, conf.hostOverride)
			for k, v := range headers {
				rt.Set(k, v)
			}
			client.Transport = rt
			opts = append(opts, elastic7.SetHttpClient(client))
		}

		return elastic7.NewClient(opts...)
	case *elastic6.Client:
		return nil, errors.New("ElasticSearch is older than 6.0.0!")
	default:
		return nil, errors.New("ElasticSearch is older than 5.0.0!")
	}
}

func assumeRoleCredentials(region, roleARN, profile string) *awscredentials.Credentials {
	sess := awssession.Must(awssession.NewSessionWithOptions(awssession.Options{
		Profile: profile,
		Config: aws.Config{
			Region:   aws.String(region),
			LogLevel: aws.LogLevel(aws.LogDebugWithHTTPBody),
			Logger: aws.LoggerFunc(func(args ...interface{}) {
				log.Print(append([]interface{}{"[DEBUG] "}, args...))
			}),
			CredentialsChainVerboseErrors: aws.Bool(true),
			MaxRetries:                    aws.Int(1),
			// HTTP client is required to fetch EC2 metadata values
			// having zero timeout on the default HTTP client sometimes makes
			// it fail with Credential error
			// https://github.com/aws/aws-sdk-go/issues/2914
			HTTPClient: &http.Client{Timeout: 10 * time.Second},
		},
		SharedConfigState: awssession.SharedConfigEnable,
	}))
	stsClient := awssts.New(sess)
	assumeRoleProvider := &awsstscreds.AssumeRoleProvider{
		Client:  stsClient,
		RoleARN: roleARN,
	}

	return awscredentials.NewChainCredentials([]awscredentials.Provider{assumeRoleProvider})
}

func awsSession(region string, conf *ProviderConf) *awssession.Session {
	sessOpts := awssession.Options{
		Config: aws.Config{
			Region: aws.String(region),
		},
	}
	// 1. access keys take priority
	// 2. next is an assume role configuration
	// 3. followed by a profile (for assume role)
	// 4. let the default credentials provider figure out the rest (env, ec2, etc..)
	//
	// note: if #1 is chosen, then no further providers will be tested, since we've overridden the credentials with just a static provider
	if conf.awsAccessKeyId != "" {
		sessOpts.Config.Credentials = awscredentials.NewStaticCredentials(conf.awsAccessKeyId, conf.awsSecretAccessKey, conf.awsSessionToken)
	} else if conf.awsAssumeRoleArn != "" {
		sessOpts.Config.Credentials = assumeRoleCredentials(region, conf.awsAssumeRoleArn, conf.awsProfile)
	} else if conf.awsProfile != "" {
		sessOpts.Profile = conf.awsProfile
		sessOpts.SharedConfigState = awssession.SharedConfigEnable
	}

	// If configured as insecure, turn off SSL verification
	if conf.insecure {
		client := &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}}
		sessOpts.Config.HTTPClient = client
	} else if conf.hostOverride != "" {
		// If we have an override for the host, a safer alternative is to set the server name.
		client := &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{ServerName: conf.hostOverride},
		}}
		sessOpts.Config.HTTPClient = client
	}

	return awssession.Must(awssession.NewSessionWithOptions(sessOpts))
}

func awsHttpClient(region string, conf *ProviderConf, headers map[string]string) *http.Client {
	session := awsSession(region, conf)
	log.Printf("[INFO] credentials: %+v", session.Config.Credentials)
	signer := awssigv4.NewSigner(session.Config.Credentials)
	log.Printf("[INFO] signer: %+v", signer)
	client, err := aws_signing_client.New(signer, session.Config.HTTPClient, "es", region)
	aws_signing_client.SetDebugLog(log.Default())
	log.Printf("[INFO] client: %+v", client)
	if err != nil {
		log.Fatal(err)
	}

	rt := WithHeader(client.Transport, conf.hostOverride)
	for k, v := range headers {
		rt.Set(k, v)
	}
	log.Printf("[INFO] rt: %+v", rt)
	client.Transport = rt

	return client
}

func tokenHttpClient(token string, tokenName string, insecure bool, headers map[string]string) *http.Client {
	client := http.DefaultClient

	rt := WithHeader(client.Transport, "")
	rt.Set("Authorization", fmt.Sprintf("%s %s", tokenName, token))
	for k, v := range headers {
		rt.Set(k, v)
	}
	client.Transport = rt

	if insecure {
		client.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true
	}

	return client
}

func tlsHttpClient(conf *ProviderConf, headers map[string]string) *http.Client {
	// Configure TLS/SSL
	tlsConfig := &tls.Config{}
	if conf.certPemPath != "" && conf.keyPemPath != "" {
		certPem, _, err := pathorcontents.Read(conf.certPemPath)
		if err != nil {
			log.Fatal(err)
		}
		keyPem, _, err := pathorcontents.Read(conf.keyPemPath)
		if err != nil {
			log.Fatal(err)
		}
		cert, err := tls.X509KeyPair([]byte(certPem), []byte(keyPem))
		if err != nil {
			log.Fatal(err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	// If a cacertFile has been specified, use that for cert validation
	if conf.cacertFile != "" {
		caCert, _, _ := pathorcontents.Read(conf.cacertFile)

		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM([]byte(caCert))
		tlsConfig.RootCAs = caCertPool
	}

	// If configured as insecure, turn off SSL verification
	if conf.insecure {
		tlsConfig.InsecureSkipVerify = true
	}

	transport := &http.Transport{TLSClientConfig: tlsConfig}

	rt := WithHeader(transport, conf.hostOverride)
	for k, v := range headers {
		rt.Set(k, v)
	}

	client := &http.Client{Transport: rt}

	return client
}
