# CLI and GitHub Action refused every function flow: the embedded runner did not declare contract_nodes

| | |
|---|---|
| **Date** | 2026-11-20 |
| **Severity** | SEV3 |
| **Status** | In review |
| **Service** | echopoint-cli, echopoint (runner capability gate) |
| **Authors** | Nanostack routine (echopoint-investigation) |
| **Duration** | ~3h 34m (trigger to resolution) |
| **Time to detect** | 3h 09m |
| **Time to mitigate** | ~3h 25m |

> Blameless: this document names systems, processes and missing guards, not people.

## Summary

Echopoint shipped input/output contract nodes for function flows to prod at 15:05 UTC (echopoint#927) and refuses an ephemeral claim of such a flow unless the runner declares `contract_nodes`. The capability arrived in runner v0.118.0, but every CLI release that day (v6.0.0 to v7.0.0) embeds runner v0.117.0 or older. So `echopoint flow run` and the GitHub Action got 409 `RUNNER_UPGRADE_REQUIRED` for every function flow, and for any flow that calls one. The flow-coverage routine found it at 18:14 UTC while writing the gate flow for #927. echopoint-cli#114 embeds runner v0.119.0 and tests every gated capability.

## Impact

Function flows with contract nodes could not run from the CLI or the GitHub Action on prod. Cloud and self-hosted runners were not affected. No customer reported it; the feature was hours old.

- **~3 h 20 m** function flows unrunnable from the CLI and Action
- **3** CLI releases shipped without the capability
- **0** gate flows that ran a function flow on the CLI

## Trigger

echopoint#927 reached prod at 15:05 UTC with a claim gate on `contract_nodes`, which no released CLI declared.

## Detection

The echopoint-investigation routine got the 409 at 18:14 UTC on the first CLI run of its new gate flow for #927. No alert covers RUNNER_UPGRADE_REQUIRED refusals.

## Resolution

echopoint-cli#114 embeds runner v0.119.0, which declares `contract_nodes`; its release lets `flow run` claim function flows.

## Timeline (UTC)

| Time | | Event |
|---|---|---|
| 14:53:02 | T−12m 09s | **echopoint#927 merges: contract nodes + claim gate** ([link](https://github.com/nanostack-dev/echopoint/pull/927)) |
| 15:01:57 | T−3m 14s | **runner#132 merges, tagged v0.117.0** ([link](https://github.com/nanostack-dev/echopoint-runner/pull/132)) |
| 15:05:11 | T+0s | **#927 live on prod; CLI claims of function flows refused** ([link](https://github.com/nanostack-dev/infra/actions/runs/44758452609)) |
| 15:05:41 | T+30s | **runner#135 declares contract_nodes (v0.118.0)** ([link](https://github.com/nanostack-dev/echopoint-runner/pull/135)) |
| 15:14:08 | T+8m 57s | **CLI#112 pins runner v0.117.0 (SSE reconnect)** ([link](https://github.com/nanostack-dev/echopoint-cli/pull/112)) |
| 15:35:23 | T+30m 12s | **CLI v7.0.0 released, still runner v0.117.0** ([link](https://github.com/nanostack-dev/echopoint-cli/releases/tag/v7.0.0)) |
| 18:14:56 | T+3h 09m | **Routine's flow run refused: 409 RUNNER_UPGRADE_REQUIRED** Execution 01a1fa2b-301a-76e1-a436-050a52df8077: `This flow uses input or output contract nodes, and the runner that claimed it cannot execute them.` |
| ~18:16 | T+3h 10m | **Cause found: CLI binary has no contract_nodes string** `go version -m` shows runner v0.117.0; `contract_nodes` landed in v0.118.0. |
| ~18:31 | T+3h 25m | **echopoint-cli#114 merged (runner v0.119.0)** ([link](https://github.com/nanostack-dev/echopoint-cli/pull/114)) |
| ~18:40 | T+3h 34m | **CLI release with contract_nodes published** |

## Root cause analysis: the five whys

Start at the impact and ask why until the answer is something the team can change. Solid edges ask why it happened; dashed edges ask why the impact was as large as it was.

```mermaid
flowchart TD
  P["`echopoint flow run` and the GitHub Action could not run any function flow on prod"]
  W1["The CLI's ephemeral claim did not declare contract_nodes, so Echopoint refused it with 409"]
  W2["CLI v7.0.0 embeds runner v0.117.0; the capability was declared in v0.118.0"]
  W3["Runner, CLI and Echopoint PRs of one feature merged within 40 min; CLI#112 pinned the runner tag that existed when it was opened"]
  W4["Echopoint gated a node type on a capability before any released CLI declared it, and nothing checks that order"]
  R1["R1 · No check or rule requires a released CLI and Action to declare a capability before Echopoint gates a node type on it"]
  W5["CLI CI passed because its capability test asserted only secret_outputs"]
  W6["The test was written for the first capability and never grew with form_body, email_wait or contract_nodes"]
  R2["R2 · The CLI test pinned one capability instead of the set Echopoint gates on"]
  W7["It went unnoticed for 3 h because no gate flow ran a function flow on the CLI runner"]
  W8["The gate runs flows on the CLI, but #927 shipped no gate flow for its new node types"]
  R3["R3 · A PR that adds a runner node type is not required to ship a gate flow that runs it on the CLI runner"]
  P -->|why?| W1
  W1 -->|why?| W2
  W2 -->|why?| W3
  W3 -->|why?| W4
  W4 -->|why?| R1
  W1 -->|why?| W5
  W5 -->|why?| W6
  W6 -->|why?| R2
  P -.->|why so bad?| W7
  W7 -->|why?| W8
  W8 -->|why?| R3
  class P problem
  class R1,R2,R3 root
  classDef problem fill:#fdecea,stroke:#c2362f,stroke-width:2px,color:#4a1210
  classDef root fill:#e7f5ec,stroke:#1f7a45,stroke-width:2px,color:#0f3a20,font-weight:bold
  classDef open fill:#fff4d9,stroke:#9a6200,stroke-dasharray:5 4,color:#3f2a00
```

- **Problem:** `echopoint flow run` and the GitHub Action could not run any function flow on prod
  - **Why?** The CLI's ephemeral claim did not declare contract_nodes, so Echopoint refused it with 409 _Evidence: Claim response `RUNNER_UPGRADE_REQUIRED … declares contract_nodes`; CLI v7.0.0 binary has no `contract_nodes` string_
    - **Why?** CLI v7.0.0 embeds runner v0.117.0; the capability was declared in v0.118.0 _Evidence: `go version -m echopoint`: runner v0.117.0; runner#135 tagged v0.118.0 at 15:06_
      - **Why?** Runner, CLI and Echopoint PRs of one feature merged within 40 min; CLI#112 pinned the runner tag that existed when it was opened _Evidence: runner v0.117.0 15:03, v0.118.0 15:06, CLI#112 merge 15:14 on v0.117.0_
        - **Why?** Echopoint gated a node type on a capability before any released CLI declared it, and nothing checks that order _Evidence: workspace AGENTS.md covers only the reverse order (runner declares before Echopoint accepts)_
          - **Why?** No check or rule requires a released CLI and Action to declare a capability before Echopoint gates a node type on it → **Root cause R1** (fixed by A2, A3)
    - **Why?** CLI CI passed because its capability test asserted only secret_outputs _Evidence: TestIntegration_EphemeralClaimDeclaresTheSecretOutputsCapability before echopoint-cli#114_
      - **Why?** The test was written for the first capability and never grew with form_body, email_wait or contract_nodes
        - **Why?** The CLI test pinned one capability instead of the set Echopoint gates on → **Root cause R2** (fixed by A1)
  - **Why so bad?** It went unnoticed for 3 h because no gate flow ran a function flow on the CLI runner _Evidence: #927 added CT only; no prod-org flow had an `input` node before 18:14_
    - **Why?** The gate runs flows on the CLI, but #927 shipped no gate flow for its new node types
      - **Why?** A PR that adds a runner node type is not required to ship a gate flow that runs it on the CLI runner → **Root cause R3** (fixed by A4, A5)

## Root causes

- **R1** No check or rule requires a released CLI and Action to declare a capability before Echopoint gates a node type on it
- **R2** The CLI test pinned one capability instead of the set Echopoint gates on
- **R3** A PR that adds a runner node type is not required to ship a gate flow that runs it on the CLI runner

## Contributing factors

- Three CLI majors (v6.0.0, v6.1.0, v7.0.0) shipped within 40 min, each focused on publish removal, not capabilities.

## Lessons learned

### What went well

- Echopoint's refusal message named the missing capability, so the cause took minutes to find.
- The gate fails closed: no CLI ran a contract node it could not execute.

### What went wrong

- The capability gate went to prod with no client able to pass it on the ephemeral path.
- The CLI capability test checked one of four capabilities.

### Where we got lucky

- The feature was hours old and had no known users; a gate on an existing node type would have broken running CI pipelines.
- The routine wrote a gate flow for #927 the same day.

## Action items

| ID | Action | Type | Owner | Due | Status | Ticket | Fixes |
|---|---|---|---|---|---|---|---|
| A1 | Embed runner v0.119.0 and assert every gated capability in the CLI claim test | prevent | Nanostack routine (echopoint-investigation) | 2026-11-20 | done | [echopoint-cli#114](https://github.com/nanostack-dev/echopoint-cli/pull/114) | R2 |
| A2 | Workspace AGENTS.md rule: an Echopoint PR that gates a node type on a capability merges after a released CLI declares it | process | Nanostack routine (echopoint-investigation) | 2026-11-20 | done |  | R1 |
| A3 | Echopoint CI check: every RunnerCapability the server gates is declared by the latest released CLI (fail before merge) | prevent | echopoint maintainers | 2026-12-04 | open |  | R1 |
| A4 | Gate flow `Function flow contract nodes` runs a function flow on the CLI runner (echopoint tag) | detect | Nanostack routine (echopoint-investigation) | 2026-11-21 | open |  | R3 |
| A5 | Alert when prod answers RUNNER_UPGRADE_REQUIRED to a released CLI version | detect | echopoint maintainers | 2026-12-04 | open |  | R3 |

## Responders

- **Investigation and fix**: Nanostack routine (echopoint-investigation)

## Messaging

### Internal

Function flows (contract nodes) could not run from the CLI/Action since #927: CLI v7.0.0 embeds runner v0.117.0 without contract_nodes. echopoint-cli#114 ships runner v0.119.0; run `echopoint update`.

### Customers

None: no known users of function flows yet.

## Supporting information

- [echopoint#927](https://github.com/nanostack-dev/echopoint/pull/927)
- [echopoint-runner#135](https://github.com/nanostack-dev/echopoint-runner/pull/135)
- [echopoint-cli#114](https://github.com/nanostack-dev/echopoint-cli/pull/114)
