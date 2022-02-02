package main

import (
	"context"

	"git.sr.ht/~emersion/gqlclient"
	"golang.org/x/oauth2"
)

type SrhtClient struct {
	GQL      *gqlclient.Client
	Endpoint string
}

func createSrhtClient(endpoint string, installation *Installation) *SrhtClient {
	tokenSrc := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: installation.SrhtToken})
	httpClient := oauth2.NewClient(context.Background(), tokenSrc)
	return &SrhtClient{
		GQL:      gqlclient.New(endpoint+"/query", httpClient),
		Endpoint: endpoint,
	}
}
