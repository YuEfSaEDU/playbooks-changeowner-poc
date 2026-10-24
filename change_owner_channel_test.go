package app

import (
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost-plugin-playbooks/server/bot"
)

// stubPluginAPI satisfies the plugin.API surface used by ChangeOwner's audit call.
type stubPluginAPI struct {
	plugin.API
}

func (s *stubPluginAPI) LogAuditRec(*model.AuditRecord) {}

// changeOwnerStore extends the existing GetPlaybookRun-only stub with the write
// operations ChangeOwner needs.
type changeOwnerStore struct {
	*stubRunStoreGetOnly
	updated      *PlaybookRun
	participants []string
}

func (s *changeOwnerStore) UpdatePlaybookRun(run *PlaybookRun) (*PlaybookRun, error) {
	s.updated = run.Clone()
	return run, nil
}

func (s *changeOwnerStore) AddParticipants(_ string, userIDs []string) error {
	s.participants = append(s.participants, userIDs...)
	return nil
}

func (s *changeOwnerStore) Follow(_, _ string) error { return nil }

func (s *changeOwnerStore) CreateTimelineEvent(event *TimelineEvent) (*TimelineEvent, error) {
	if event.ID == "" {
		event.ID = "timeline-event-id"
	}
	return event, nil
}

type changeOwnerPoster struct {
	bot.Poster
	dmUser    string
	wsChannel string
}

func (p *changeOwnerPoster) DM(userID string, _ *model.Post) error {
	p.dmUser = userID
	return nil
}

func (p *changeOwnerPoster) PublishWebsocketEventToChannelReliable(_ string, _ interface{}, channelID string) {
	p.wsChannel = channelID
}

func (p *changeOwnerPoster) PublishWebsocketEventToUserReliable(_ string, _ interface{}, _ string) {}

// TestChangeOwnerAllowsNonChannelMemberTeamUser demonstrates that ChangeOwner
// validates only team membership for a team run, not membership in the run's
// private channel. The new owner is not in the private channel, yet the owner
// assignment succeeds, AddParticipants records them, and RunView grants them
// access solely because they are now the owner.
func TestChangeOwnerAllowsNonChannelMemberTeamUser(t *testing.T) {
	const (
		teamID      = "team-1"
		channelID   = "private-channel-1"
		runID       = "run-1"
		playbookID  = "playbook-1"
		oldOwnerID  = "old-owner"
		newOwnerID  = "new-owner"
		requesterID = "requester"
	)

	run := &PlaybookRun{
		ID:             runID,
		PlaybookID:     playbookID,
		TeamID:         teamID,
		ChannelID:      channelID,
		OwnerUserID:    oldOwnerID,
		ParticipantIDs: []string{oldOwnerID},
		Type:           RunTypePlaybook,
	}

	store := &changeOwnerStore{stubRunStoreGetOnly: &stubRunStoreGetOnly{run: run}}
	poster := &changeOwnerPoster{}
	api := &plugintest.API{}
	client := pluginapi.NewClient(api, nil)

	api.On("GetUser", oldOwnerID).Return(&model.User{Id: oldOwnerID, Username: "old-owner"}, nil)
	api.On("GetUser", newOwnerID).Return(&model.User{Id: newOwnerID, Username: "new-owner"}, nil)
	api.On("GetUser", requesterID).Return(&model.User{Id: requesterID, Username: "requester"}, nil)
	api.On("GetTeamMember", teamID, newOwnerID).Return(&model.TeamMember{TeamId: teamID, UserId: newOwnerID}, nil)
	api.On("GetChannel", channelID).Return(&model.Channel{Id: channelID, TeamId: teamID, Type: model.ChannelTypePrivate}, nil)
	api.On("HasPermissionToChannel", requesterID, channelID, model.PermissionManagePrivateChannelMembers).Return(false)
	api.On("GetChannelMembersByIds", channelID, mock.Anything).Return(model.ChannelMembers{}, nil).Maybe()

	svc := &PlaybookRunServiceImpl{
		pluginAPI:      client,
		store:          store,
		poster:         poster,
		configService:  &stubConfigService{incrementalUpdates: false},
		permissions:    &PermissionsService{pluginAPI: client},
		licenseChecker: &stubLicenseCheckerNoAttributes{},
		api:            &stubPluginAPI{},
	}

	require.NoError(t, svc.ChangeOwner(runID, requesterID, newOwnerID))
	require.NotNil(t, store.updated)
	require.Equal(t, newOwnerID, store.updated.OwnerUserID)
	require.Contains(t, store.participants, newOwnerID)

	// The new owner is a team member but not a member of the private channel.
	// RunView short-circuits on OwnerUserID, so no channel/playbook check runs.
	api.On("HasPermissionToTeam", newOwnerID, teamID, model.PermissionViewTeam).Return(true)
	permSvc := &PermissionsService{
		pluginAPI:       client,
		runService:      &stubRunService{run: store.updated},
		playbookService: &stubPlaybookService{playbook: Playbook{ID: playbookID, TeamID: teamID, Public: false}},
	}
	require.NoError(t, permSvc.RunView(newOwnerID, runID))
}
