package client

import (
	"context"
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	"github.com/skilld-labs/opcua"
	"github.com/skilld-labs/opcua/errors"
	"github.com/skilld-labs/opcua/ua"
	"github.com/skilld-labs/telemetry-opcua-exporter/config"
	"github.com/skilld-labs/telemetry-opcua-exporter/log"
)

func NewClientFromServerConfig(cfg config.ServerConfig, logger log.Logger) *opcua.Client {
	opts := []opcua.Option{}
	authMode := *cfg.Auth
	switch authMode {
	case "Certificate":
		return NewCertificateClient(*cfg.CertFile, *cfg.KeyFile, cfg, logger)
	case "UserName":
		return NewBasicAuthClient(*cfg.Username, *cfg.Password, cfg, logger)
	default:
		*cfg.Auth = "Anonymous"
		opts = append(opts, opcua.AuthAnonymous())
	}
	return getClientWithOptions(cfg, opts, logger)
}

func NewCertificateClient(certfile, keyfile string, cfg config.ServerConfig, logger log.Logger) *opcua.Client {
	return getClientWithOptions(cfg, certificateOptions(certfile, keyfile, logger), logger)
}

func NewBasicAuthClient(username, password string, cfg config.ServerConfig, logger log.Logger) *opcua.Client {
	return getClientWithOptions(cfg, []opcua.Option{opcua.AuthUsername(username, password)}, logger)
}

func getClientWithOptions(cfg config.ServerConfig, opts []opcua.Option, logger log.Logger) *opcua.Client {
	clientSecurityOpts, err := clientSecurityOptions(cfg, logger)
	if err != nil {
		logger.Err("%v", err)
	}
	opts = append(opts, clientSecurityOpts...)
	opts = append(opts, opcua.Lifetime(10*time.Minute), opcua.RequestTimeout(5*time.Second), opcua.AutoReconnect(true))

	c := opcua.NewClient(*cfg.Endpoint, opts...)
	if err := c.Connect(context.Background()); err != nil {
		logger.Err("cannot connect opcua client %v", err)
	}
	return c
}

func certificateOptions(certfile, keyfile string, logger log.Logger) []opcua.Option {
	opts := []opcua.Option{}
	if keyfile != "" {
		opts = append(opts, opcua.PrivateKeyFile(keyfile))
	}
	cert, err := ioutil.ReadFile(certfile)
	if err != nil {
		logger.Err("cannot read certfile : %v", err)
	}
	if certfile != "" {
		opts = append(opts, opcua.Certificate(cert))
	}
	return opts
}

func clientSecurityOptions(cfg config.ServerConfig, logger log.Logger) ([]opcua.Option, error) {
	savePolicy := *cfg.Policy
	endpoints, err := opcua.GetEndpoints(*cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("%v", err)
	}
	authMode := ua.UserTokenTypeFromString(*cfg.Auth)
	opts := []opcua.Option{}
	var secPolicy string
	switch {
	case *cfg.Policy == "auto":
	case strings.HasPrefix(*cfg.Policy, ua.SecurityPolicyURIPrefix):
		secPolicy = *cfg.Policy
		*cfg.Policy = ""
	case *cfg.Policy == "None" || *cfg.Policy == "Basic128Rsa15" || *cfg.Policy == "Basic256" || *cfg.Policy == "Basic256Sha256" || *cfg.Policy == "Aes128_Sha256_RsaOaep" || *cfg.Policy == "Aes256_Sha256_RsaPss":
		secPolicy = ua.SecurityPolicyURIPrefix + *cfg.Policy
		*cfg.Policy = ""
	default:
		return nil, fmt.Errorf("invalid security.Policy: %s", *cfg.Policy)
	}

	var secMode ua.MessageSecurityMode
	switch strings.ToLower(*cfg.Mode) {
	case "auto":
	case "none":
		secMode = ua.MessageSecurityModeNone
		*cfg.Mode = ""
	case "sign":
		secMode = ua.MessageSecurityModeSign
		*cfg.Mode = ""
	case "signandencrypt":
		secMode = ua.MessageSecurityModeSignAndEncrypt
		*cfg.Mode = ""
	default:
		return nil, fmt.Errorf("invalid security.Mode: %s", *cfg.Mode)
	}

	// Allow input of only one of sec.Mode,sec.Policy when choosing 'None'
	if secMode == ua.MessageSecurityModeNone || secPolicy == ua.SecurityPolicyURINone {
		secMode = ua.MessageSecurityModeNone
		secPolicy = ua.SecurityPolicyURINone
	}

	// Find the best endpoint based on our input and server recommendation (highest SecurityMode+SecurityLevel)
	var serverEndpoint *ua.EndpointDescription
	switch {
	case *cfg.Mode == "auto" && *cfg.Policy == "auto": // No user selection, choose best
		for _, e := range endpoints {
			if serverEndpoint == nil || (e.SecurityMode >= serverEndpoint.SecurityMode && e.SecurityLevel >= serverEndpoint.SecurityLevel) {
				serverEndpoint = e
			}
		}

	case *cfg.Mode != "auto" && *cfg.Policy == "auto": // User only cares about.Mode, select highest securitylevel with that.Mode
		for _, e := range endpoints {
			if e.SecurityMode == secMode && (serverEndpoint == nil || e.SecurityLevel >= serverEndpoint.SecurityLevel) {
				serverEndpoint = e
			}
		}

	case *cfg.Mode == "auto" && *cfg.Policy != "auto": // User only cares about.Policy, select highest securitylevel with that.Policy
		for _, e := range endpoints {
			if e.SecurityPolicyURI == secPolicy && (serverEndpoint == nil || e.SecurityLevel >= serverEndpoint.SecurityLevel) {
				serverEndpoint = e
			}
		}

	default: // User cares about both
		for _, e := range endpoints {
			if e.SecurityPolicyURI == secPolicy && e.SecurityMode == secMode && (serverEndpoint == nil || e.SecurityLevel >= serverEndpoint.SecurityLevel) {
				serverEndpoint = e
			}
		}
	}

	*cfg.Policy = savePolicy

	if serverEndpoint == nil {
		return nil, fmt.Errorf("unable to find suitable server.Endpoint with selected sec.Policy and sec.Mode")
	}

	if validateEndpointConfig(endpoints, serverEndpoint.SecurityPolicyURI, serverEndpoint.SecurityMode, authMode); err != nil {
		return nil, fmt.Errorf("error validating input: %s", err)
	}

	opts = append(opts, opcua.SecurityFromEndpoint(serverEndpoint, authMode))
	logger.Info("using config: Endpoint: %s, Security.Mode: %s, %s auth.Mode : %s", serverEndpoint.EndpointURL, serverEndpoint.SecurityPolicyURI, serverEndpoint.SecurityMode, authMode)
	return opts, nil
}

func validateEndpointConfig(endpoints []*ua.EndpointDescription, secPolicy string, secMode ua.MessageSecurityMode, authMode ua.UserTokenType) error {
	for _, e := range endpoints {
		if e.SecurityMode == secMode && e.SecurityPolicyURI == secPolicy {
			for _, t := range e.UserIdentityTokens {
				if t.TokenType == authMode {
					return nil
				}
			}
		}
	}
	return errors.Errorf("server does not support an.Endpoint with security : %s , %s", secPolicy, secMode)
}
