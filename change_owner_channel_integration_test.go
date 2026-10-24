package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost-plugin-playbooks/client"
)

// TestChangeOwner_NonChannelMemberTeamUser reproduces the ChangeOwner authz gap:
// a team member who is not a member of the run's private channel can be assigned
// as owner, after which RunView grants them access solely because they are owner.
func TestChangeOwner_NonChannelMemberTeamUser(t *testing.T) {
	e := Setup(t)
	e.CreateBasic()

	ctx := context.Background()

	// The fixture's private channel is created by AdminUser. RegularUser2 is a
	// member of the team but not of this private channel.
	_, _, err := e.ServerAdminClient.GetChannelMember(ctx, e.BasicPrivateChannel.Id, e.RegularUser2.Id, "")
	require.Error(t, err, "fixture precondition: RegularUser2 must not be in the private channel")

	createPublicRun := false
	run, err := e.PlaybooksAdminClient.PlaybookRuns.Create(ctx, client.PlaybookRunCreateOptions{
		Name:            "Private channel owner change",
		OwnerUserID:     e.AdminUser.Id,
		TeamID:          e.BasicTeam.Id,
		ChannelID:       e.BasicPrivateChannel.Id,
		PlaybookID:      e.BasicPlaybook.ID,
		CreatePublicRun: &createPublicRun,
	})
	require.NoError(t, err)
	require.Equal(t, e.AdminUser.Id, run.OwnerUserID)

	// AdminUser is the current owner and a channel member. ChangeOwner validates
	// only team membership for the new owner, not private-channel membership.
	err = e.PlaybooksAdminClient.PlaybookRuns.ChangeOwner(ctx, run.ID, e.RegularUser2.Id)
	require.NoError(t, err)

	// RegularUser2 can now read the run through the API even though they were
	// never added to the private channel.
	got, err := e.PlaybooksClient2.PlaybookRuns.Get(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, e.RegularUser2.Id, got.OwnerUserID)
}
