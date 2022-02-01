package main

import (
	"context"
	"log"
	"os"

	"git.sr.ht/~emersion/gqlclient"
	"golang.org/x/oauth2"
)

type SrhtClient struct {
	GQL      *gqlclient.Client
	Endpoint string
}

func createSrhtClient() *SrhtClient {
	endpoint := os.Getenv("SRHT_ENDPOINT")
	if endpoint == "" {
		endpoint = "https://builds.sr.ht"
	}
	token := os.Getenv("SRHT_TOKEN")
	if token == "" {
		log.Fatalf("missing SRHT_TOKEN")
	}
	tokenSrc := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	httpClient := oauth2.NewClient(context.Background(), tokenSrc)
	return &SrhtClient{
		GQL:      gqlclient.New(endpoint+"/query", httpClient),
		Endpoint: endpoint,
	}
}
