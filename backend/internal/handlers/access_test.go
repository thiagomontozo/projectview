package handlers

import (
	"testing"

	"github.com/google/uuid"

	"projectview/internal/models"
)

func user(role string) *models.User {
	return &models.User{ID: uuid.New(), Role: role}
}

func TestIsAdmin(t *testing.T) {
	if !isAdmin(user(models.RoleAdmin)) {
		t.Error("admin not recognized as admin")
	}
	for _, role := range []string{models.RoleManager, models.RoleMember, "", "root"} {
		if isAdmin(user(role)) {
			t.Errorf("role %q wrongly recognized as admin", role)
		}
	}
	if isAdmin(nil) {
		t.Error("nil user recognized as admin")
	}
}

// Members join existing work; opening new workstreams is a privileged action.
func TestCanAdministerStructure(t *testing.T) {
	cases := map[string]bool{
		models.RoleAdmin:   true,
		models.RoleManager: true,
		models.RoleMember:  false,
		"":                 false,
	}
	for role, want := range cases {
		if got := canAdministerStructure(user(role)); got != want {
			t.Errorf("canAdministerStructure(%q) = %v, want %v", role, got, want)
		}
	}
	if canAdministerStructure(nil) {
		t.Error("nil user allowed to create structure")
	}
}

func TestIsProjectMember(t *testing.T) {
	owner := user(models.RoleMember)
	member := user(models.RoleMember)
	outsider := user(models.RoleMember)

	project := &models.Project{Owner: &owner.ID, Members: []uuid.UUID{member.ID}}

	if !isProjectMember(project, owner) {
		t.Error("owner not treated as a member")
	}
	if !isProjectMember(project, member) {
		t.Error("listed member not treated as a member")
	}
	if isProjectMember(project, outsider) {
		t.Error("outsider treated as a member")
	}
	if isProjectMember(nil, member) || isProjectMember(project, nil) {
		t.Error("nil arguments treated as membership")
	}
}

// Whether someone may touch the tasks inside a project.
func TestCanWorkOnProject(t *testing.T) {
	owner := user(models.RoleMember)
	member := user(models.RoleMember)
	outsider := user(models.RoleMember)
	admin := user(models.RoleAdmin)
	// A manager who is not on the project has no business editing its tasks.
	strangerManager := user(models.RoleManager)

	project := &models.Project{Owner: &owner.ID, Members: []uuid.UUID{member.ID}}

	cases := []struct {
		name string
		u    *models.User
		want bool
	}{
		{"owner", owner, true},
		{"member", member, true},
		{"admin", admin, true},
		{"outsider", outsider, false},
		{"manager who is not a member", strangerManager, false},
	}
	for _, tc := range cases {
		if got := canWorkOnProject(project, tc.u); got != tc.want {
			t.Errorf("canWorkOnProject(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Managing is strictly narrower than working: deleting a project cascades to
// every task in it, so a plain member must never be able to do it.
func TestCanManageProject(t *testing.T) {
	owner := user(models.RoleMember)
	member := user(models.RoleMember)
	admin := user(models.RoleAdmin)
	managerMember := user(models.RoleManager)
	managerOutsider := user(models.RoleManager)

	project := &models.Project{
		Owner:   &owner.ID,
		Members: []uuid.UUID{member.ID, managerMember.ID},
	}

	cases := []struct {
		name string
		u    *models.User
		want bool
	}{
		{"owner", owner, true},
		{"admin", admin, true},
		{"manager who is a member", managerMember, true},
		{"plain member", member, false},
		{"manager who is not a member", managerOutsider, false},
	}
	for _, tc := range cases {
		if got := canManageProject(project, tc.u); got != tc.want {
			t.Errorf("canManageProject(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Every account that can manage a project must also be able to work in it;
// the reverse must not hold. This is the invariant that keeps the two
// predicates from drifting apart as the rules evolve.
func TestManageImpliesWorkButNotViceVersa(t *testing.T) {
	owner := user(models.RoleMember)
	member := user(models.RoleMember)
	project := &models.Project{Owner: &owner.ID, Members: []uuid.UUID{member.ID}}

	for _, u := range []*models.User{owner, member, user(models.RoleAdmin), user(models.RoleManager)} {
		if canManageProject(project, u) && !canWorkOnProject(project, u) {
			t.Errorf("role %q can manage but not work on the project", u.Role)
		}
	}
	if !canWorkOnProject(project, member) || canManageProject(project, member) {
		t.Error("a plain member should be able to work on but not manage the project")
	}
}

func TestCanManageTeam(t *testing.T) {
	lead := user(models.RoleMember)
	other := user(models.RoleMember)
	admin := user(models.RoleAdmin)
	manager := user(models.RoleManager)

	team := &models.Team{LeadID: &lead.ID, Members: []uuid.UUID{lead.ID, other.ID}}

	if !canManageTeam(team, lead) {
		t.Error("team lead cannot manage their own team")
	}
	if !canManageTeam(team, admin) {
		t.Error("admin cannot manage a team")
	}
	// Being a member, or a manager elsewhere, is not enough.
	if canManageTeam(team, other) {
		t.Error("ordinary member can manage the team")
	}
	if canManageTeam(team, manager) {
		t.Error("unrelated manager can manage the team")
	}

	leaderless := &models.Team{Members: []uuid.UUID{other.ID}}
	if canManageTeam(leaderless, other) {
		t.Error("member can manage a team that has no lead")
	}
	if !canManageTeam(leaderless, admin) {
		t.Error("admin cannot manage a leaderless team")
	}
}

func TestCanEditUser(t *testing.T) {
	self := user(models.RoleMember)
	admin := user(models.RoleAdmin)
	stranger := user(models.RoleMember)
	manager := user(models.RoleManager)

	if !canEditUser(self.ID, self) {
		t.Error("user cannot edit their own profile")
	}
	if !canEditUser(self.ID, admin) {
		t.Error("admin cannot edit another user")
	}
	// This is the hole that let anyone rewrite anyone else's profile.
	if canEditUser(self.ID, stranger) {
		t.Error("unrelated user can edit someone else's profile")
	}
	if canEditUser(self.ID, manager) {
		t.Error("manager can edit an unrelated user's profile")
	}
	if canEditUser(self.ID, nil) {
		t.Error("nil user can edit a profile")
	}
}

// Guards the exact shape of the escalation that was possible before: an
// ordinary member acting on a project they have nothing to do with.
func TestOutsiderCannotTouchForeignProject(t *testing.T) {
	outsider := user(models.RoleMember)
	strangerOwner := uuid.New()
	project := &models.Project{
		Owner:   &strangerOwner,
		Members: []uuid.UUID{uuid.New()},
	}

	if canWorkOnProject(project, outsider) {
		t.Error("outsider can create or edit tasks in a foreign project")
	}
	if canManageProject(project, outsider) {
		t.Error("outsider can reconfigure or delete a foreign project")
	}
}
