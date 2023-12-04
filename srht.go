package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"git.sr.ht/~emersion/go-oauth2"
	"git.sr.ht/~emersion/gqlclient"

	"git.sr.ht/~emersion/hottub/buildssrht"
)

func getSrhtOAuth2Client(metasrhtEndpoint, clientID, clientSecret string) (*oauth2.Client, error) {
	metadata, err := oauth2.DiscoverServerMetadata(context.Background(), metasrhtEndpoint)
	if err != nil {
		return nil, err
	}

	return &oauth2.Client{
		Server:       metadata,
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}, nil
}

type SrhtClient struct {
	GQL      *gqlclient.Client
	Endpoint string
}

func createSrhtClient(endpoint string, oauth2Client *oauth2.Client, installation *Installation) *SrhtClient {
	httpClient := oauth2Client.NewHTTPClient(&oauth2.TokenResp{
		AccessToken: installation.SrhtToken,
		TokenType:   oauth2.TokenTypeBearer,
	})
	return &SrhtClient{
		GQL:      gqlclient.New(endpoint+"/query", httpClient),
		Endpoint: endpoint,
	}
}

func saveSrhtToken(ctx context.Context, db *DB, srhtEndpoint string, oauth2Client *oauth2.Client, installation *Installation, tokenResp *oauth2.TokenResp) error {
	installation.SrhtToken = tokenResp.AccessToken
	installation.SrhtRefreshToken = tokenResp.RefreshToken
	installation.SrhtTokenExpiresAt = time.Now().Add(tokenResp.ExpiresIn)
	srht := createSrhtClient(srhtEndpoint, oauth2Client, installation)
	user, err := buildssrht.FetchUser(srht.GQL, ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch sr.ht user: %v", err)
	}

	if err := db.StoreInstallation(installation); err != nil {
		return fmt.Errorf("failed to store installation: %v", err)
	}

	log.Printf("user %v has completed installation %v", user.CanonicalName, installation.ID)
	return nil
}
