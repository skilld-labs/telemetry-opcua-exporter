package client

import (
	"crypto/rsa"
	"crypto/tls"
	"strings"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
	"github.com/skilld-labs/telemetry-opcua-exporter/config"
	"github.com/skilld-labs/telemetry-opcua-exporter/log"
)

func NewClientFromServerConfig(c config.ServerConfig, l log.Logger) *opcua.Client {
	e := findEndpoint(c, l)
	crt := loadCertificate(c, l)

	o := []opcua.Option{}
	o = append(o, connectionOptions()...)
	o = append(o, authenticationOptions(c, l, e, &crt)...)
	o = append(o, securityOptions(c, l, e, &crt)...)

	l.Info("client using config: Endpoint: %s, Security Mode: %s, %s, Authentication Mode : %s", e.EndpointURL, e.SecurityPolicyURI, e.SecurityMode, c.AuthMode)

	return opcua.NewClient(c.Endpoint, o...)
}

func findEndpoint(c config.ServerConfig, l log.Logger) *ua.EndpointDescription {
	ee, err := opcua.GetEndpoints(c.Endpoint)
	if err != nil {
		l.Fatal("get endpoints failed: %v", err)
	}

	var policy string
	switch {
	case c.SecPolicy == "auto":
	case strings.HasPrefix(c.SecPolicy, ua.SecurityPolicyURIPrefix):
		policy = c.SecPolicy
	case c.SecPolicy == "None" ||
		c.SecPolicy == "Basic128Rsa15" ||
		c.SecPolicy == "Basic256" ||
		c.SecPolicy == "Basic256Sha256" ||
		c.SecPolicy == "Aes128_Sha256_RsaOaep" ||
		c.SecPolicy == "Aes256_Sha256_RsaPss":
		policy = ua.SecurityPolicyURIPrefix + c.SecPolicy
	default:
		l.Fatal("invalid security policy: %s", c.SecPolicy)
	}

	var mode ua.MessageSecurityMode
	switch strings.ToLower(c.SecMode) {
	case "auto":
	case "none":
		mode = ua.MessageSecurityModeNone
	case "sign":
		mode = ua.MessageSecurityModeSign
	case "signandencrypt":
		mode = ua.MessageSecurityModeSignAndEncrypt
	default:
		l.Fatal("invalid security mode: %s", c.SecMode)
	}

	// Allow input of only one of security mode or security policy when choosing 'None'
	if mode == ua.MessageSecurityModeNone || policy == ua.SecurityPolicyURINone {
		mode = ua.MessageSecurityModeNone
		policy = ua.SecurityPolicyURINone
	}

	// Find the best endpoint based on our input and server recommendation (highest SecurityMode+SecurityLevel)
	var ep *ua.EndpointDescription
	switch {
	case c.SecMode == "auto" && c.SecPolicy == "auto": // No user selection, choose best
		for _, e := range ee {
			if ep == nil || (e.SecurityMode >= ep.SecurityMode && e.SecurityLevel >= ep.SecurityLevel) {
				ep = e
			}
		}

	case c.SecMode != "auto" && c.SecPolicy == "auto": // User only cares about.Mode, select highest securitylevel with that.Mode
		for _, e := range ee {
			if e.SecurityMode == mode && (ep == nil || e.SecurityLevel >= ep.SecurityLevel) {
				ep = e
			}
		}

	case c.SecMode == "auto" && c.SecPolicy != "auto": // User only cares about.Policy, select highest securitylevel with that.Policy
		for _, e := range ee {
			if e.SecurityPolicyURI == policy && (ep == nil || e.SecurityLevel >= ep.SecurityLevel) {
				ep = e
			}
		}

	default: // User cares about both
		for _, e := range ee {
			if e.SecurityPolicyURI == policy && e.SecurityMode == mode && (ep == nil || e.SecurityLevel >= ep.SecurityLevel) {
				ep = e
			}
		}
	}

	if ep != nil {
		// Make sure the selected endpoint supports the authentication mode
		utt := ua.UserTokenTypeFromString(c.AuthMode)
		for _, t := range ep.UserIdentityTokens {
			if t.TokenType == utt {
				return ep
			}
		}
	}

	l.Fatal("unable to find suitable server endpoint with selected security policy, security mode and authentication mode")
	return nil
}

func connectionOptions() []opcua.Option {
	return []opcua.Option{
		opcua.Lifetime(10 * time.Minute),
		opcua.RequestTimeout(5 * time.Second),
		opcua.AutoReconnect(true),
	}
}

func authenticationOptions(c config.ServerConfig, l log.Logger, e *ua.EndpointDescription, crt *tls.Certificate) []opcua.Option {
	o := []opcua.Option{}
	switch c.AuthMode {
	case "Certificate":
		o = append(o, opcua.AuthCertificate(crt.Certificate[0]))
	case "UserName":
		o = append(o, opcua.AuthUsername(c.Username, c.Password))
	default:
		l.Info("authentication mode not set, defaulting to anonymous")
		o = append(o, opcua.AuthAnonymous())
	}
	o = append(o, opcua.SecurityFromEndpoint(e, ua.UserTokenTypeFromString(c.AuthMode)))
	return o
}

func securityOptions(c config.ServerConfig, l log.Logger, e *ua.EndpointDescription, crt *tls.Certificate) []opcua.Option {
	o := []opcua.Option{}
	switch c.SecMode {
	case "Sign", "SignAndEncrypt":
		o = append(o,
			opcua.PrivateKey(crt.PrivateKey.(*rsa.PrivateKey)),
			opcua.Certificate(crt.Certificate[0]))
	default:
		l.Warn("No security mode is not recommended, consider using one")
	}
	return o
}

func loadCertificate(c config.ServerConfig, l log.Logger) tls.Certificate {
	var crt tls.Certificate
	if c.CertPath != "" && c.KeyPath != "" {
		crt, err := tls.LoadX509KeyPair(c.CertPath, c.KeyPath)
		if err != nil {
			l.Fatal("failed to load certificate: %s", err)
		} else {
			_, ok := crt.PrivateKey.(*rsa.PrivateKey)
			if !ok {
				l.Fatal("invalid private key")
			}
		}
		return crt
	}
	return crt
}
