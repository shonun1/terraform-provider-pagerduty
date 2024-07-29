package pagerduty

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/logging"
	"github.com/heimweh/go-pagerduty/pagerduty"
	"github.com/heimweh/go-pagerduty/persistentconfig"
)

// Config defines the configuration options for the PagerDuty client
type Config struct {
	mu sync.Mutex

	// The PagerDuty API URL
	ApiUrl string

	// Override default PagerDuty API URL
	ApiUrlOverride string

	// The PagerDuty APP URL
	AppUrl string

	// The PagerDuty API V2 token
	Token string

	// The PagerDuty User level token for Slack
	UserToken string

	// Skip validation of the token against the PagerDuty API
	SkipCredsValidation bool

	// UserAgent for API Client
	UserAgent string

	// Do not verify TLS certs for HTTPS requests - useful if you're behind a corporate proxy
	InsecureTls bool

	APITokenType *pagerduty.AuthTokenType

	AppOauthScopedTokenParams *persistentconfig.AppOauthScopedTokenParams

	ServiceRegion string

	client      *pagerduty.Client
	slackClient *pagerduty.Client
}

const invalidCreds = `

No valid credentials found for PagerDuty provider.
Please see https://www.terraform.io/docs/providers/pagerduty/index.html
for more information on providing credentials for this provider.
`

// Client returns a PagerDuty client, initializing when necessary.
func (c *Config) Client() (*pagerduty.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Return the previously-configured client if available.
	if c.client != nil {
		return c.client, nil
	}

	// Validate that the PagerDuty token is set
	if c.Token == "" && c.APITokenType != nil && *c.APITokenType == pagerduty.AuthTokenTypeAPIToken {
		return nil, fmt.Errorf(invalidCreds)
	}

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   25 * time.Second,
			KeepAlive: 20 * time.Second,
		}).DialContext,
		TLSClientConfig: &tls.Config{
			CipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			},
		},
		TLSHandshakeTimeout:   20 * time.Second,
		MaxIdleConns:          0,
		MaxIdleConnsPerHost:   500,
		MaxConnsPerHost:       0,
		IdleConnTimeout:       1 * time.Minute,
		ResponseHeaderTimeout: 20 * time.Second,
	}

	if c.InsecureTls {
		transport.TLSClientConfig.InsecureSkipVerify = true
	}

	httpClient := &http.Client{
		Transport: logging.NewTransport("PagerDuty", transport),
		Timeout:   2 * time.Minute,
	}

	apiUrl := c.ApiUrl
	if c.ApiUrlOverride != "" {
		apiUrl = c.ApiUrlOverride
	}

	config := &pagerduty.Config{
		BaseURL:                   apiUrl,
		Debug:                     logging.IsDebugOrHigher(),
		HTTPClient:                httpClient,
		Token:                     c.Token,
		UserAgent:                 c.UserAgent,
		AppOauthScopedTokenParams: c.AppOauthScopedTokenParams,
		APIAuthTokenType:          c.APITokenType,
	}

	client, err := pagerduty.NewClient(config)
	if err != nil {
		return nil, err
	}

	if !c.SkipCredsValidation {
		// Validate the credentials by calling the abilities endpoint,
		// if we get a 401 response back we return an error to the user
		if err := client.ValidateAuth(); err != nil {
			return nil, fmt.Errorf(fmt.Sprintf("%s\n%s", err, invalidCreds))
		}
	}

	c.client = client

	log.Printf("[INFO] PagerDuty client configured")

	return c.client, nil
}

func (c *Config) SlackClient() (*pagerduty.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Return the previously-configured client if available.
	if c.slackClient != nil {
		return c.slackClient, nil
	}

	// Validate that the user level PagerDuty token is set
	if c.UserToken == "" {
		return nil, fmt.Errorf(invalidCreds)
	}

	var httpClient *http.Client
	httpClient = http.DefaultClient

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if c.InsecureTls {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	httpClient.Transport = logging.NewTransport("PagerDuty", transport)

	config := &pagerduty.Config{
		BaseURL:    c.AppUrl,
		Debug:      logging.IsDebugOrHigher(),
		HTTPClient: httpClient,
		Token:      c.UserToken,
		UserAgent:  c.UserAgent,
	}

	client, err := pagerduty.NewClient(config)
	if err != nil {
		return nil, err
	}

	c.slackClient = client

	log.Printf("[INFO] PagerDuty client configured for slack")

	return c.slackClient, nil
}
