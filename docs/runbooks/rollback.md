# Rollback

Restore the last verified CLI release through the installer's `--version` option or the corresponding platform archive, then verify `echopoint version` and the affected command. Preserve profile configuration and credentials; restoring a binary does not restore resources changed through the API.

For Action consumers, pin a verified immutable Action commit and its matching `cli-version` in the calling workflow, then rerun the scoped flow suite. Repointing a shared floating tag changes other consumers, so restore individual workflow pins first while preparing a corrective CLI release.

Revert the source regression through a focused PR with a release-triggering commit for behavioral fixes. Verify that its generated client, embedded runner/kit, MCP catalog and Action contract are compatible before publication. Record version and flow-suite evidence; API-side recovery belongs to the resource's owning service.
