package integration_test

import (
	"crypto/ed25519"

	"github.com/anytty/anytty/cloud/controller/account"
)

func newIntegrationAccountService(config account.Config) (*account.Service, error) {
	config.AccessSigningKey = ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	config.AccessSigningKeyID = "account-integration-test-key"
	config.AccessIssuer = "https://cloud.example.test"
	config.AccessAudience = "anytty-cloud-web"
	return account.New(config)
}
