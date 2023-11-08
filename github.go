package main

import (
	"log"
	"net/http"
	"strconv"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v56/github"
)

func createAppsTransport(appIDStr, privateKeyFilename string) *ghinstallation.AppsTransport {
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		log.Fatalf("invalid app ID: %v", err)
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
