# Normal release flow restoration

Validated against both live stacks on 2026-11-06 with Pulumi **3.188.0**, matching
GitHub Actions. These were refreshed, read-only previews. No deployment, merge,
release publication or Kubernetes mutation was performed. The restoration takes
effect after the workflow/code change is merged to `main`.

## Result

| Environment | Application | Other planned operations | Delete/replace |
| --- | --- | --- | ---: |
| Staging | Update the old experiment image to the current published main digest; update deploy-commit annotation | Recreate two expired ingress admission jobs; normalize two empty Grafana RBAC rule lists | 0 |
| Production | Keep release `1.8.7`; no pod-template change | Same two admission jobs and two RBAC normalizations | 0 |

Staging's summary was 2 creates, 3 updates and 183 unchanged resources;
production's was 2 creates, 2 updates and 188 unchanged resources. This is not a
zero-operation production preview, but it contains no application, database,
node, storage, networking, controller deployment or monitoring-configuration
change. The detailed sanitized evidence is [release-flow-preview.json](release-flow-preview.json).

The admission jobs previously completed and were removed by their zero-second
TTL. Their desired definitions use the already-reviewed patched chart labels and
`kube-webhook-certgen` 1.6.9 image; the older completed checkpoint recorded 1.6.0.
Both job definitions passed the existing November security-upgrade policy against
that checkpoint. They are not controller downgrades or a new gateway rollout.
The Grafana Role and ClusterRole change only `rules: null` to `rules: []`, granting
no permissions.

CNPG remains **1.30.1**, ingress-nginx **1.15.1**, Grafana **12.4.14**, and both
PostgreSQL clusters remain on **17.5**. Existing CPU/memory budgets, worker/database
placement, autoscaling, connection pools, persistent storage, compression and log
sampling are preserved. Production stack configuration is unchanged. Staging
stack configuration removes only its image pin, rollout annotation pin and the
associated comment.

## Restored behavior

- Main pushes build and deploy staging using that build's exact image digest.
  The `main` image tag continues to be published; concurrent builds moving that
  tag cannot change the image selected by a particular automatic deployment.
- Manual staging previews/applies resolve the published `main` tag to one digest.
  `stagingImage` remains an optional explicit diagnostic override, rather than a
  required permanent experiment pin.
- Publishing a release still builds versioned images and artifacts. Production
  promotion still comes from merging an explicit `imageTag` change to `main`.
- The normal workflows no longer dispatch through one-off capacity, compression
  or security-patch allowlists. Those tools remain available separately.
- Main-only apply checks, GitHub environment-scoped credentials/protections,
  production scaling preflight and saved-plan applies remain in place. Raw
  events/plans are not written to public logs or uploaded as Actions artifacts.

## Saved-plan compatibility

The full previews expose the existing Helm v3 behavior where newly recreated
admission Job references have empty IDs in chart outputs. The deployment runner
constrains those two output references to their fixed Kubernetes namespace/name
IDs, after checking the Job type, parent, explicit name and reference URN. It
changes neither resource inputs nor operations. Tests cover identity mismatch,
idempotency and preservation of resource goals. No broad resource-scope exception
or disabled saved-plan check was introduced.

Both final previews ran through the actual deployment runner, including this
compatibility check. Applying the saved plan has not been exercised against live
staging or production in this restoration task.

## Verification

- All deployment Go tests and `go vet ./...` passed.
- All 72 deployment Python tests passed, including normal application-change
  reporting, private-value omission, preview failure handling, main-only CI
  applies, digest validation and the Helm reference correction.
- Both changed workflows passed actionlint 1.7.7.
- Both final refreshed live previews passed; every changed resource and field
  was inspected. The production scaling preflight passed.
- Before/after remote Pulumi checkpoint exports were identical.
- `git diff --check` passed.

Raw state, plans and preview events remain in the owner-only local directory
`/tmp/registry-release-flow`, outside Git. Only sanitized evidence is committed
with the implementation when reviewed. The separate Gateway Fabric/Vector
research changes remain pending; this release-flow change does not deploy them.
