//go:build generate

package buildssrht

import (
	_ "git.sr.ht/~emersion/gqlclient/cmd/gqlclientgen"
)

//go:generate go run git.sr.ht/~emersion/gqlclient/cmd/gqlclientgen -s schema.graphqls -q operations.graphql -o gql.go
