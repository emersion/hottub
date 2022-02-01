package main

import (
	"context"
	"os"

	"git.sr.ht/~emersion/gqlclient"
	"golang.org/x/oauth2"
)

type SrhtClient struct {
	GQL      *gqlclient.Client
	Endpoint string
}

func createSrhtClient(installation *Installation) *SrhtClient {
	endpoint := os.Getenv("SRHT_ENDPOINT")
	if endpoint == "" {
		endpoint = "https://builds.sr.ht"
	}
	tokenSrc := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: installation.SrhtToken})
	httpClient := oauth2.NewClient(context.Background(), tokenSrc)
	return &SrhtClient{
		GQL:      gqlclient.New(endpoint+"/query", httpClient),
		Endpoint: endpoint,
	}
}
