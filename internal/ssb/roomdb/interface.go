package roomdb

import (
	"context"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

type PrivacyMode int

const (
	ModeOpen       PrivacyMode = 0
	ModeCommunity  PrivacyMode = 1
	ModeRestricted PrivacyMode = 2
	ModeUnknown    PrivacyMode = -1
)

func ParsePrivacyMode(s string) PrivacyMode {
	switch s {
	case "open":
		return ModeOpen
	case "community":
		return ModeCommunity
	case "restricted":
		return ModeRestricted
	default:
		return ModeUnknown
	}
}

type Role int

const (
	RoleUnknown Role = iota - 1
	RoleNone
	RoleMember
	RoleModerator
	RoleAdmin
)

func (r Role) String() string {
	switch r {
	case RoleMember:
		return "member"
	case RoleModerator:
		return "moderator"
	case RoleAdmin:
		return "admin"
	default:
		return "unknown"
	}
}

type Member struct {
	ID     int64
	PubKey refs.FeedRef
	Role   Role
}

type Alias struct {
	ID         int64
	Name       string
	Owner      refs.FeedRef
	Signature  []byte
	ReversePTR string
}

type Invite struct {
	ID        int64
	Token     string
	CreatedBy int64
	UsedBy    *refs.FeedRef
	UsedAt    int64
	CreatedAt int64
	Active    bool
}

type ListEntry struct {
	ID      int64
	PubKey  refs.FeedRef
	Comment string
	AddedAt int64
}

type RoomConfig interface {
	GetPrivacyMode(ctx context.Context) (PrivacyMode, error)
	SetPrivacyMode(ctx context.Context, mode PrivacyMode) error
	GetDefaultLanguage(ctx context.Context) (string, error)
	SetDefaultLanguage(ctx context.Context, lang string) error
}

type AuthFallbackService interface {
	Check(ctx context.Context, username, password string) (int64, error)
	SetPassword(ctx context.Context, memberID int64, password string) error
	CreateResetToken(ctx context.Context, createdByMember, forMember int64) (string, error)
	SetPasswordWithToken(ctx context.Context, resetToken, password string) error
}

type AuthWithSSBService interface {
	CreateToken(ctx context.Context, memberID int64) (string, error)
	CheckToken(ctx context.Context, token string) (int64, error)
	RemoveToken(ctx context.Context, token string) error
	WipeTokensForMember(ctx context.Context, memberID int64) error
}

type MembersService interface {
	Add(ctx context.Context, pubKey refs.FeedRef, role Role) (int64, error)
	GetByID(ctx context.Context, id int64) (Member, error)
	GetByFeed(ctx context.Context, feed refs.FeedRef) (Member, error)
	List(ctx context.Context) ([]Member, error)
	Count(ctx context.Context) (uint, error)
	RemoveFeed(ctx context.Context, feed refs.FeedRef) error
	RemoveID(ctx context.Context, id int64) error
	SetRole(ctx context.Context, id int64, role Role) error
}

type DeniedKeysService interface {
	Add(ctx context.Context, ref refs.FeedRef, comment string) error
	HasFeed(ctx context.Context, ref refs.FeedRef) bool
	HasID(ctx context.Context, id int64) bool
	GetByID(ctx context.Context, id int64) (ListEntry, error)
	List(ctx context.Context) ([]ListEntry, error)
	Count(ctx context.Context) (uint, error)
	RemoveFeed(ctx context.Context, ref refs.FeedRef) error
	RemoveID(ctx context.Context, id int64) error
}

type AliasesService interface {
	Resolve(ctx context.Context, alias string) (Alias, error)
	GetByID(ctx context.Context, id int64) (Alias, error)
	List(ctx context.Context) ([]Alias, error)
	Register(ctx context.Context, alias string, userFeed refs.FeedRef, signature []byte) error
	Revoke(ctx context.Context, alias string) error
}

type InvitesService interface {
	Create(ctx context.Context, createdBy int64) (string, error)
	Consume(ctx context.Context, token string, newMember refs.FeedRef) (Invite, error)
	GetByToken(ctx context.Context, token string) (Invite, error)
	GetByID(ctx context.Context, id int64) (Invite, error)
	List(ctx context.Context) ([]Invite, error)
	Count(ctx context.Context, onlyActive bool) (uint, error)
	Revoke(ctx context.Context, id int64) error
}

type RuntimeAttendantSnapshot struct {
	ID          refs.FeedRef
	Addr        string
	ConnectedAt int64
	LastSeenAt  int64
	Active      bool
}

type RuntimeTunnelEndpointSnapshot struct {
	Target      refs.FeedRef
	Addr        string
	AnnouncedAt int64
	LastSeenAt  int64
	Active      bool
}

type RuntimeSnapshotsService interface {
	MarkAllInactive(ctx context.Context) error
	UpsertAttendant(ctx context.Context, id refs.FeedRef, addr string, connectedAt int64) error
	DeactivateAttendant(ctx context.Context, id refs.FeedRef) error
	ListAttendants(ctx context.Context, onlyActive bool) ([]RuntimeAttendantSnapshot, error)
	UpsertTunnelEndpoint(ctx context.Context, target refs.FeedRef, addr string, announcedAt int64) error
	DeactivateTunnelEndpoint(ctx context.Context, target refs.FeedRef) error
	ListTunnelEndpoints(ctx context.Context, onlyActive bool) ([]RuntimeTunnelEndpointSnapshot, error)
}
