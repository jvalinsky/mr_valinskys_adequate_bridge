package handlers

import (
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
)

func TestRoomInvitePolicyMatrix(t *testing.T) {
	tests := []struct {
		name       string
		mode       roomdb.PrivacyMode
		role       roomdb.Role
		canCreate  bool
		canRevoke  bool
		canAlias   bool
		canMembers bool
		canDenied  bool
	}{
		{name: "open anonymous", mode: roomdb.ModeOpen, role: roomdb.RoleNone, canCreate: true, canRevoke: false, canAlias: false, canMembers: false, canDenied: false},
		{name: "open member", mode: roomdb.ModeOpen, role: roomdb.RoleMember, canCreate: true, canRevoke: true, canAlias: true, canMembers: false, canDenied: false},
		{name: "community member", mode: roomdb.ModeCommunity, role: roomdb.RoleMember, canCreate: true, canRevoke: true, canAlias: true, canMembers: false, canDenied: false},
		{name: "restricted member", mode: roomdb.ModeRestricted, role: roomdb.RoleMember, canCreate: false, canRevoke: false, canAlias: false, canMembers: false, canDenied: false},
		{name: "restricted moderator", mode: roomdb.ModeRestricted, role: roomdb.RoleModerator, canCreate: true, canRevoke: true, canAlias: false, canMembers: false, canDenied: true},
		{name: "restricted admin", mode: roomdb.ModeRestricted, role: roomdb.RoleAdmin, canCreate: true, canRevoke: true, canAlias: false, canMembers: true, canDenied: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := canCreateInvite(tc.mode, tc.role); got != tc.canCreate {
				t.Fatalf("canCreateInvite = %t, want %t", got, tc.canCreate)
			}
			if got := canRevokeInvite(tc.mode, tc.role); got != tc.canRevoke {
				t.Fatalf("canRevokeInvite = %t, want %t", got, tc.canRevoke)
			}
			if got := canRevokeAlias(tc.mode, tc.role); got != tc.canAlias {
				t.Fatalf("canRevokeAlias = %t, want %t", got, tc.canAlias)
			}
			if got := canMutateMembers(tc.mode, tc.role); got != tc.canMembers {
				t.Fatalf("canMutateMembers = %t, want %t", got, tc.canMembers)
			}
			if got := canMutateDenied(tc.mode, tc.role); got != tc.canDenied {
				t.Fatalf("canMutateDenied = %t, want %t", got, tc.canDenied)
			}
		})
	}
}

func TestParseRoomMemberRole(t *testing.T) {
	tests := []struct {
		in   string
		role roomdb.Role
		ok   bool
	}{
		{in: "member", role: roomdb.RoleMember, ok: true},
		{in: "moderator", role: roomdb.RoleModerator, ok: true},
		{in: "admin", role: roomdb.RoleAdmin, ok: true},
		{in: "invalid", role: roomdb.RoleUnknown, ok: false},
	}

	for _, tc := range tests {
		got, err := parseRoomMemberRole(tc.in)
		if tc.ok && err != nil {
			t.Fatalf("parseRoomMemberRole(%q) unexpected error: %v", tc.in, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("parseRoomMemberRole(%q) expected error", tc.in)
		}
		if got != tc.role {
			t.Fatalf("parseRoomMemberRole(%q) = %v, want %v", tc.in, got, tc.role)
		}
	}
}

func TestOpenSQLiteRoomOpsProviderRequiresRoomSQLite(t *testing.T) {
	if _, err := OpenSQLiteRoomOpsProvider(t.TempDir(), "", roomdb.RoleAdmin, nil); err == nil {
		t.Fatalf("expected missing sqlite error")
	}
}
