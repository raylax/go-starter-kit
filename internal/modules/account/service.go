package account

import (
	"fmt"
	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"time"
)

type Options struct {
	IdleTTL, MaxTTL time.Duration
	FrontendURL     string
}

type Database interface {
	sqlc.DBTX
	db.Beginner
}
type Service struct {
	database Database
	queries  *store
	options  Options
	deps     Dependencies
}

func NewService(database Database, options Options, deps Dependencies) (*Service, error) {
	if database == nil || options.IdleTTL < time.Minute || options.MaxTTL < options.IdleTTL || deps.Passwords == nil || deps.Authorizer == nil || deps.Logger == nil {
		return nil, fmt.Errorf("账户服务依赖不完整")
	}
	if deps.Federation == nil {
		deps.Federation = disabledFederation{}
	}
	return &Service{database: database, queries: newStore(database), options: options, deps: deps}, nil
}
