package room

import (
	"context"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
)

// RoomDB defines the database interface required by the room runtime.
type RoomDB interface {
	Members() roomdb.MembersService
	Aliases() roomdb.AliasesService
	Invites() roomdb.InvitesService
	DeniedKeys() roomdb.DeniedKeysService
	RoomConfig() roomdb.RoomConfig
	AuthFallback() roomdb.AuthFallbackService
	AuthTokens() roomdb.AuthWithSSBService
	RuntimeSnapshots() roomdb.RuntimeSnapshotsService
	Close() error
}

// ActiveBridgeAccountLister lists active bridged accounts for the room UI.
type ActiveBridgeAccountLister interface {
	ListActiveBridgedAccountsWithStats(ctx context.Context) ([]db.BridgedAccountStats, error)
}

// ActiveBridgeAccountDetailer gets details for a single bridged account.
type ActiveBridgeAccountDetailer interface {
	GetActiveBridgedAccountWithStats(ctx context.Context, atDID string) (*db.BridgedAccountStats, error)
	ListRecentPublishedMessagesByDID(ctx context.Context, atDID string, limit int) ([]db.Message, error)
}
