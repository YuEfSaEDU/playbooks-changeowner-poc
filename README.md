# Playbooks ChangeOwner private-channel authz proof

Pinned target: `mattermost/mattermost-plugin-playbooks` @ `1da42cf8cfdd5e838d9e08f0893e7444938d2fc7`.

`TestChangeOwnerAllowsNonChannelMemberTeamUser` demonstrates that `PlaybookRunServiceImpl.ChangeOwner` validates only team membership for a team run, not membership in the run's private channel. The new owner is not a channel member, yet the assignment succeeds and `RunView` grants access solely because the user is now the owner.

Run: `go test ./server/app -run 'TestChangeOwnerAllowsNonChannelMemberTeamUser|TestRunChangeOwner' -v`
