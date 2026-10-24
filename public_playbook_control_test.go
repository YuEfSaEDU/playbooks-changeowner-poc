package main

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost-plugin-playbooks/client"
)

// TestPublicPlaybookPreChangeControl is the decisive control for HYP-MM-001:
// if a public playbook's run in a private channel is readable by a team member
// BEFORE any owner change, then run visibility is not bound to private-channel
// membership and the ChangeOwner finding is not a channel-boundary bypass.
func TestPublicPlaybookPreChangeControl(t *testing.T) {
	e := Setup(t)
	e.CreateBasic()

	ctx := context.Background()

	_, _, err := e.ServerAdminClient.GetChannelMember(ctx, e.BasicPrivateChannel.Id, e.RegularUser2.Id, "")
	require.Error(t, err, "fixture precondition: RegularUser2 must not be in the private channel")
	t.Logf("control: GetChannelMember(private=%s, user=%s) -> error: %v", e.BasicPrivateChannel.Id, e.RegularUser2.Id, err)

	createPublicRun := false
	run, err := e.PlaybooksAdminClient.PlaybookRuns.Create(ctx, client.PlaybookRunCreateOptions{
		Name:            "Public playbook pre-change control",
		OwnerUserID:     e.AdminUser.Id,
		TeamID:          e.BasicTeam.Id,
		ChannelID:       e.BasicPrivateChannel.Id,
		PlaybookID:      e.BasicPlaybook.ID, // public playbook
		CreatePublicRun: &createPublicRun,
	})
	require.NoError(t, err)

	serverClient2 := model.NewAPIv4Client(e.ServerClient.URL)
	_, _, err = serverClient2.Login(ctx, e.RegularUser2.Email, testUserPassword)
	require.NoError(t, err)

	resp, err := e.DoPluginAPIRequestWithHeaders(ctx, serverClient2, http.MethodGet, "/api/v0/runs/"+run.ID, "", nil)
	if err != nil {
		if resp != nil {
			t.Logf("public playbook pre-change GET -> status %d, error: %v", resp.StatusCode, err)
		} else {
			t.Logf("public playbook pre-change GET -> error: %v", err)
		}
		t.Fatalf("expected 200 pre-change for a public playbook run; got error")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	t.Logf("public playbook pre-change GET -> %d\n%s", resp.StatusCode, string(body))
	require.Equal(t, http.StatusOK, resp.StatusCode)
}
