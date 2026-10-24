package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
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
	t.Logf("control: GetChannelMember(private=%s, user=%s) -> error: %v", e.BasicPrivateChannel.Id, e.RegularUser2.Id, err)

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
	t.Logf("POST /runs/%s/owner as AdminUser -> owner_id=%s (200)", run.ID, e.RegularUser2.Id)

	// RegularUser2 can now read the run through the API even though they were
	// never added to the private channel.
	serverClient2 := model.NewAPIv4Client(e.ServerClient.URL)
	_, _, err = serverClient2.Login(ctx, e.RegularUser2.Email, testUserPassword)
	require.NoError(t, err)
	resp, err := e.DoPluginAPIRequestWithHeaders(ctx, serverClient2, http.MethodGet, "/api/v0/runs/"+run.ID, "", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	t.Logf("GET /api/v0/runs/%s as RegularUser2 -> %d\n%s", run.ID, resp.StatusCode, string(body))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &raw))
	require.Equal(t, e.RegularUser2.Id, raw["owner_user_id"])
	require.Equal(t, e.BasicPrivateChannel.Id, raw["channel_id"])
	require.Equal(t, "Private channel owner change", raw["name"])

	got, err := e.PlaybooksClient2.PlaybookRuns.Get(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, e.RegularUser2.Id, got.OwnerUserID)
}
