package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"git.sr.ht/~emersion/gqlclient"
	"github.com/emersion/go-oauth2"

	"github.com/emersion/hottub/buildssrht"
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
	populateSrhtInstallation(installation, tokenResp)
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

func refreshSrhtToken(ctx context.Context, db *DB, oauth2Client *oauth2.Client, installation *Installation) error {
	if installation.SrhtRefreshToken == "" || installation.SrhtTokenExpiresAt.IsZero() {
		return nil
	}
	if time.Until(installation.SrhtTokenExpiresAt) > 15*24*time.Hour {
		return nil
	}

	tokenResp, err := oauth2Client.Refresh(ctx, installation.SrhtRefreshToken, nil)
	if err != nil {
		return err
	}

	populateSrhtInstallation(installation, tokenResp)
	if err := db.StoreInstallation(installation); err != nil {
		return fmt.Errorf("failed to store installation: %v", err)
	}

	log.Printf("refreshed sr.ht token for installation %v", installation.ID)
	return nil
}

func populateSrhtInstallation(installation *Installation, tokenResp *oauth2.TokenResp) {
	installation.SrhtToken = tokenResp.AccessToken
	installation.SrhtRefreshToken = tokenResp.RefreshToken
	installation.SrhtTokenExpiresAt = time.Now().Add(tokenResp.ExpiresIn)
}
