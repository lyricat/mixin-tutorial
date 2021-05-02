package main

import (
	"time"

	"github.com/patrickmn/go-cache"
)

var (
	// Set the default expiration to 10 minutes
	SessionCache = cache.New(time.Minute*10, cache.NoExpiration)
)

const (
	// the init state
	UserSessionStateInit = 0
	// the state that users tell bot what them want
	UserSessionStateSpecifiedSymbol = 1
)

type UserSession struct {
	State          int
	ConversationID string
	// Symbol and AssetID will be available when State == UserSessionStateSpecifiedSymbol
	Symbol  string
	AssetID string
}

func setSession(userID string, sess *UserSession) {
	SessionCache.Set(userID, sess, cache.DefaultExpiration)
}

func getSession(userID string) *UserSession {
	if x, found := SessionCache.Get(userID); found {
		return x.(*UserSession)
	}
	return nil
}
