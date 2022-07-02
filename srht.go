package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"git.sr.ht/~emersion/gqlclient"
	"golang.org/x/oauth2"

	"git.sr.ht/~emersion/hottub/buildssrht"
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

func exchangeSrhtOAuth2(ctx context.Context, endpoint, code, clientID, clientSecret string) (token string, err error) {
	conf := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL:  endpoint,
			AuthStyle: oauth2.AuthStyleInHeader,
		},
	}
	tok, err := conf.Exchange(ctx, code)
	if err != nil {
		return "", err
	}
	if t := tok.Type(); !strings.EqualFold(t, "Bearer") {
		return "", fmt.Errorf("unsupported OAuth2 token type: %v", t)
	}

	return tok.AccessToken, nil
}

func saveSrhtToken(ctx context.Context, db *DB, srhtEndpoint string, installation *Installation, token string) error {
	installation.SrhtToken = token
	srht := createSrhtClient(srhtEndpoint, installation)
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
