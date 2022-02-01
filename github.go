package main

import (
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v42/github"
)

func createAppsTransport() *ghinstallation.AppsTransport {
	appID, err := strconv.ParseInt(os.Getenv("GITHUB_APP_IDENTIFIER"), 10, 64)
	if err != nil {
		log.Fatalf("invalid GITHUB_APP_IDENTIFIER: %v", err)
	}
	privateKeyFilename := os.Getenv("GITHUB_PRIVATE_KEY")
	if privateKeyFilename == "" {
		log.Fatalf("missing GITHUB_PRIVATE_KEY")
	}
	atr, err := ghinstallation.NewAppsTransportKeyFromFile(http.DefaultTransport, appID, privateKeyFilename)
	if err != nil {
		log.Fatalf("failed to read app private key: %v", err)
	}
	return atr
}

func newInstallationClient(atr *ghinstallation.AppsTransport, installation *github.Installation) *github.Client {
	itr := ghinstallation.NewFromAppsTransport(atr, *installation.ID)
	return github.NewClient(&http.Client{Transport: itr})
}
