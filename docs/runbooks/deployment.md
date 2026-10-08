# Publication and consumer installation

The CLI publishes artifacts rather than a long-lived service. [release.yml](../../.github/workflows/release.yml) runs after pushes to `main`: breaking Conventional Commits select major, `feat` minor, `fix`/`perf` patch, and only documentation/chore/CI/refactor/test commits select no release. Verify the selected tag and GoReleaser assets before claiming a behavior is available.

[GoReleaser](../../.goreleaser.yml) builds the platform archives and checksums. A successful release also repoints the floating Action contract tag `v1` at that commit; its tag-moving job is part of release completion. The Action's `cli-version` input defaults to `latest`, so pin it when reproducibility matters.

Users install via [install.sh](../../install.sh), [install.ps1](../../install.ps1) or release archives as described in [README](../../README.md). The Unix installer accepts `--version`; self-update selects the latest release. Verify `echopoint version`, `echopoint --help`, and a scoped representative command against the intended profile/organization. For embedded runner changes, record both CLI and runner versions plus linked API/UI companion PRs. A merged docs PR alone creates no CLI release.
