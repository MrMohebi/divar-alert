// Package platform names the messaging platforms the bot serves.
package platform

import "fmt"

// Platform is a messaging platform with a Telegram-compatible Bot API.
type Platform string

const (
	Telegram Platform = "telegram"
	Bale     Platform = "bale"
)

// All lists every supported platform in a stable order.
var All = []Platform{Telegram, Bale}

// Valid reports whether p is a supported platform.
func (p Platform) Valid() bool {
	return p == Telegram || p == Bale
}

// Chat identifies one chat. Chat IDs are only unique within a platform, so a
// chat is always addressed by both.
type Chat struct {
	Platform Platform
	ID       int64
}

func (c Chat) String() string {
	return fmt.Sprintf("%s:%d", c.Platform, c.ID)
}
