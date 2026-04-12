package room

import (
	"sync"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

// testFeed returns a deterministic FeedRef for testing. Each distinct
// byte value produces a unique feed.
func testFeed(b byte) refs.FeedRef {
	var id [32]byte
	id[0] = b
	return *refs.MustNewFeedRef(id[:], refs.RefAlgoFeedSSB1)
}

// --- Attendant tests ---

func TestAddAttendant(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	alice := testFeed(1)

	rs.AddAttendant(alice, RoleMember)
	attendants := rs.GetAttendants()
	if len(attendants) != 1 {
		t.Fatalf("expected 1 attendant, got %d", len(attendants))
	}
	if !attendants[0].Feed.Equal(alice) {
		t.Error("wrong attendant feed")
	}
}

func TestAddAttendant_Dedup(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	alice := testFeed(1)

	rs.AddAttendant(alice, RoleMember)
	rs.AddAttendant(alice, RoleModerator)

	attendants := rs.GetAttendants()
	if len(attendants) != 1 {
		t.Fatalf("expected 1 attendant after dedup, got %d", len(attendants))
	}
	if attendants[0].Role != RoleModerator {
		t.Error("expected role to be updated to Moderator")
	}
}

func TestRemoveAttendant(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	alice := testFeed(1)

	rs.AddAttendant(alice, RoleMember)
	rs.RemoveAttendant(alice)

	if len(rs.GetAttendants()) != 0 {
		t.Error("expected no attendants after removal")
	}
}

func TestRemoveAttendant_NotFound(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	rs.RemoveAttendant(testFeed(99)) // should not panic
}

func TestGetAttendants_Empty(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	if len(rs.GetAttendants()) != 0 {
		t.Error("expected empty attendant list")
	}
}

func TestGetAttendants_Multiple(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	rs.AddAttendant(testFeed(1), RoleMember)
	rs.AddAttendant(testFeed(2), RoleModerator)
	rs.AddAttendant(testFeed(3), RoleAdmin)

	attendants := rs.GetAttendants()
	if len(attendants) != 3 {
		t.Errorf("expected 3 attendants, got %d", len(attendants))
	}
}

// --- Member tests ---

func TestAddMember(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	alice := testFeed(1)

	if err := rs.AddMember(alice, RoleMember); err != nil {
		t.Fatal(err)
	}
	if !rs.IsMember(alice) {
		t.Error("expected alice to be a member")
	}
}

func TestAddMember_Overwrite(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	alice := testFeed(1)

	rs.AddMember(alice, RoleMember)
	rs.AddMember(alice, RoleAdmin)

	if rs.GetMemberRole(alice) != RoleAdmin {
		t.Error("expected role to be updated to Admin")
	}
}

func TestRemoveMember(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	alice := testFeed(1)

	rs.AddMember(alice, RoleMember)
	rs.RemoveMember(alice)

	if rs.IsMember(alice) {
		t.Error("expected alice to no longer be a member")
	}
}

func TestRemoveMember_NotFound(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	rs.RemoveMember(testFeed(99)) // should not panic
}

func TestIsMember_True(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	rs.AddMember(testFeed(1), RoleMember)
	if !rs.IsMember(testFeed(1)) {
		t.Error("expected IsMember to return true")
	}
}

func TestIsMember_False(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	if rs.IsMember(testFeed(1)) {
		t.Error("expected IsMember to return false")
	}
}

func TestGetMemberRole_Found(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	rs.AddMember(testFeed(1), RoleAdmin)

	if rs.GetMemberRole(testFeed(1)) != RoleAdmin {
		t.Error("expected RoleAdmin")
	}
}

func TestGetMemberRole_NotFound(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	if rs.GetMemberRole(testFeed(1)) != -1 {
		t.Error("expected -1 for missing member")
	}
}

// --- Alias tests ---

func TestAddAlias(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	alice := testFeed(1)

	if err := rs.AddAlias(alice, "alice"); err != nil {
		t.Fatal(err)
	}

	resolved := rs.ResolveAlias("alice")
	if resolved == nil {
		t.Fatal("expected alias to resolve")
	}
	if !resolved.Equal(alice) {
		t.Error("alias resolved to wrong feed")
	}
}

func TestAddAlias_Overwrite(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	alice := testFeed(1)
	bob := testFeed(2)

	rs.AddAlias(alice, "shared")
	rs.AddAlias(bob, "shared")

	resolved := rs.ResolveAlias("shared")
	if resolved == nil {
		t.Fatal("expected alias to resolve")
	}
	if !resolved.Equal(bob) {
		t.Error("expected alias to point to bob after overwrite")
	}
}

func TestRemoveAlias(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	rs.AddAlias(testFeed(1), "alice")
	rs.RemoveAlias("alice")

	if rs.ResolveAlias("alice") != nil {
		t.Error("expected alias to be removed")
	}
}

func TestRemoveAlias_NotFound(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	rs.RemoveAlias("nonexistent") // should not panic
}

func TestResolveAlias_Found(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	rs.AddAlias(testFeed(1), "alice")

	if rs.ResolveAlias("alice") == nil {
		t.Error("expected alias to resolve")
	}
}

func TestResolveAlias_Missing(t *testing.T) {
	rs := NewRoomState(testFeed(0))

	if rs.ResolveAlias("nobody") != nil {
		t.Error("expected nil for missing alias")
	}
}

func TestGetAliases_List(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	alice := testFeed(1)

	rs.AddAlias(alice, "alice1")
	rs.AddAlias(alice, "alice2")

	aliases := rs.GetAliases(alice)
	if len(aliases) != 2 {
		t.Errorf("expected 2 aliases, got %d", len(aliases))
	}
}

func TestGetAliases_Empty(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	aliases := rs.GetAliases(testFeed(1))
	if len(aliases) != 0 {
		t.Errorf("expected 0 aliases, got %d", len(aliases))
	}
}

func TestGetAliases_FiltersByFeed(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	alice := testFeed(1)
	bob := testFeed(2)

	rs.AddAlias(alice, "alice")
	rs.AddAlias(bob, "bob")

	aliceAliases := rs.GetAliases(alice)
	if len(aliceAliases) != 1 {
		t.Errorf("expected 1 alias for alice, got %d", len(aliceAliases))
	}
}

// --- DenyKey tests ---

func TestDenyKey(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	rs.DenyKey("badkey123")

	if !rs.IsDenied("badkey123") {
		t.Error("expected key to be denied")
	}
}

func TestDenyKey_Idempotent(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	rs.DenyKey("badkey123")
	rs.DenyKey("badkey123")

	if !rs.IsDenied("badkey123") {
		t.Error("expected key to still be denied")
	}
}

func TestAllowKey(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	rs.DenyKey("badkey123")
	rs.AllowKey("badkey123")

	if rs.IsDenied("badkey123") {
		t.Error("expected key to be allowed after AllowKey")
	}
}

func TestAllowKey_NotDenied(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	rs.AllowKey("notdenied") // should not panic
}

func TestIsDenied_False(t *testing.T) {
	rs := NewRoomState(testFeed(0))
	if rs.IsDenied("unknown") {
		t.Error("expected false for unknown key")
	}
}

// --- Concurrency test ---

func TestRoomState_Concurrent(t *testing.T) {
	rs := NewRoomState(testFeed(0))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n byte) {
			defer wg.Done()
			feed := testFeed(n)
			rs.AddAttendant(feed, RoleMember)
			rs.AddMember(feed, RoleMember)
			rs.IsMember(feed)
			rs.GetAttendants()
			rs.GetMemberRole(feed)
			rs.RemoveAttendant(feed)
			rs.RemoveMember(feed)
		}(byte(i + 1))
	}
	wg.Wait()
}
