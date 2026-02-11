# OpenClaw BOSH Release & Ops Manager Tile

## Overview

`bosh-openclaw` is a BOSH release and Tanzu Operations Manager tile that deploys [OpenClaw](https://github.com/openclaw/openclaw) AI agents as a managed, multi-tenant service on Cloud Foundry.

- **Dedicated VMs per developer** -- each OpenClaw agent runs on its own BOSH-managed VM with full OS-level isolation, persistent disk, and a dedicated WebChat UI on its own route
- **Self-service provisioning** -- developers create agents via `cf create-service openclaw developer my-agent` and receive a personal URL (e.g., `openclaw-alice.apps.example.com`)
- **Enterprise security controls** -- centralized version enforcement, skills allowlisting, sandbox policies, network segmentation, and per-instance SSO via `p-identity`
- **Curated skills management** -- a dedicated Skills Registry syncs approved skills from ClawHub with security scanning and quarantine
- **Implements Open Service Broker API v2.16** -- fully async provisioning with last_operation polling

## Architecture

```
Tanzu Operations Manager / BOSH Director
  |
  | deploys
  v
+-------------------------------------------------------------+
|                    BOSH Deployment                           |
|                                                             |
|  STATIC INFRASTRUCTURE                                      |
|  +----------------+  +----------------+  +----------------+ |
|  | openclaw-      |  | openclaw-      |  | openclaw-admin-| |
|  | broker (1 VM)  |  | registry (1 VM)|  | dashboard(1 VM)| |
|  | + route-reg    |  |                |  | + route-reg    | |
|  +-------+--------+  +----------------+  +----------------+ |
|          |                                                   |
|          | provisions on-demand via BOSH Director API         |
|          v                                                   |
|  ON-DEMAND AGENT VMs (one per developer/team)               |
|  +---------------------+  +---------------------+          |
|  | openclaw-agent       |  | openclaw-agent       |  ...   |
|  | - OpenClaw runtime   |  | - OpenClaw runtime   |        |
|  | - WebChat UI         |  | - WebChat UI         |        |
|  | - Gateway WebSocket  |  | - Gateway WebSocket  |        |
|  | - route-registrar    |  | - route-registrar    |        |
|  | - sso-proxy (opt)    |  | - sso-proxy (opt)    |        |
|  | Route: openclaw-alice|  | Route: openclaw-bob  |        |
|  +---------------------+  +---------------------+          |
|                                                             |
|  CF Go Router <-- routes from each agent's route-registrar  |
+-------------------------------------------------------------+
```

**Key flow:** The Service Broker (Go) registers with the CF marketplace. When a developer runs `cf create-service openclaw developer my-agent`, the broker provisions a dedicated agent VM via the BOSH Director API. Each agent VM runs the OpenClaw runtime, a WebChat UI, a Gateway WebSocket endpoint, and optional browser automation. A colocated Route Registrar gives each agent a dedicated route through the CF Go Router. The Skills Registry manages curated skills from ClawHub. The Admin Dashboard provides fleet monitoring for operators.

## Prerequisites

- **BOSH Director** or **Tanzu Ops Manager 3.0+**
- **Cloud Foundry** with Go Router and NATS
- **Stemcell:** ubuntu-jammy 1.*
- **LLM provider:** Tanzu GenAI Service, Anthropic (direct), or OpenAI (direct)
- **(Optional)** `p-identity` service for per-instance SSO

## Quick Start (BOSH)

```bash
# 1. Download OpenClaw and Node.js blobs
./scripts/add-blob.sh 2026.2.10

# 2. Create the BOSH release
bosh create-release --force

# 3. Upload release to the director
bosh upload-release

# 4. Upload the stemcell
bosh upload-stemcell \
  https://bosh.io/d/stemcells/bosh-google-kvm-ubuntu-jammy-go_agent

# 5. Deploy (full deployment with broker, registry, and dashboard)
bosh -d openclaw deploy manifests/openclaw.yml \
  -v broker_username=openclaw-broker \
  -v broker_password=changeme \
  -v bosh_director_url=https://10.0.0.6:25555 \
  -v bosh_client_id=admin \
  -v bosh_client_secret=... \
  -v bosh_ca_cert=... \
  -v genai_endpoint=https://genai.example.com \
  -v genai_api_key=... \
  -v apps_domain=apps.example.com \
  -v nats_machines='[10.0.0.10]' \
  -v nats_port=4222 \
  -v nats_user=nats \
  -v nats_password=...

# 6. Register the broker with Cloud Foundry
cf create-service-broker openclaw openclaw-broker changeme \
  https://openclaw-broker.apps.example.com

# 7. Enable access to plans
cf enable-service-access openclaw

# 8. Developers can now provision agents
cf create-service openclaw developer my-agent
```

A minimal deployment (broker only, no dashboard or registry) is available at `manifests/openclaw-lite.yml`.

## Quick Start (Ops Manager Tile)

```bash
# 1. Build the tile
./scripts/build-tile.sh
# This runs: bosh create-release, then tile build
# Output: product/openclaw-<version>.pivotal

# 2. Import to Ops Manager
# Upload the .pivotal file via the Ops Manager UI or:
om upload-product -p product/openclaw-*.pivotal
om stage-product -p openclaw

# 3. Configure tile forms in the Ops Manager UI:
#    - GenAI / LLM Configuration
#    - Security & Compliance
#    - Skills Management
#    - SSO / Authentication
#    - Service Broker
#    - Networking

# 4. Apply Changes
# The tile will deploy the broker, registry, and dashboard,
# then run the register-broker and smoke-tests errands.
```

## Service Plans

| Plan | VM Type | RAM | Disk | Features |
|---|---|---|---|---|
| `developer` | small | 2 GB | 10 GB | Dedicated WebChat UI, cloud LLM, per-instance SSO |
| `developer-plus` | medium | 4 GB | 20 GB | All of developer + browser automation, all messaging channels |
| `team` | large | 8 GB | 50 GB | All of developer-plus + multi-user, Slack/Teams channels |

Plans can be individually enabled or disabled via the tile's Service Broker configuration form. Instance limits are configurable globally (default: 50 total) and per-org (default: 10).

## Configuration Reference

The Ops Manager tile exposes six configuration forms:

| Form | Description |
|---|---|
| **GenAI / LLM Configuration** | Select LLM provider (Tanzu GenAI Service, Anthropic, or OpenAI), set API keys and model |
| **Security & Compliance** | Sandbox mode (strict/moderate/loose), Control UI toggle, browser automation, blocked commands, minimum OpenClaw version, outbound network blocking |
| **Skills Management** | Skills policy (allowlist/blocklist/disabled), approved skills list, ClawHub auto-sync, security scanning on sync |
| **SSO / Authentication** | Enable per-instance SSO via p-identity, allowed email domains, session timeout |
| **Service Broker** | Broker credentials, plan enablement, max instances (total and per-org) |
| **Networking** | Apps domain, route prefixes for agents/broker/dashboard, agent VM network name |

## Security

### CVE-2026-25253 Mitigations

CVE-2026-25253 (CVSS 8.8) is a 1-click RCE via WebSocket token exfiltration through the Control UI. The tile implements five layers of defense:

1. **Version gating** -- the broker refuses to provision agents running versions below 2026.1.29 (configurable via the Minimum OpenClaw Version setting)
2. **Control UI disabled by default** -- the WebSocket-based Control UI is off unless explicitly enabled by the operator
3. **Authentication required** -- when Control UI is enabled, `require_auth` is enforced
4. **WebSocket origin check** -- strict `Origin` header validation prevents cross-site WebSocket hijacking
5. **Network isolation** -- agent VMs are not directly internet-reachable; all access routes through the CF Go Router

### Sandbox Modes

| Mode | Behavior |
|---|---|
| `strict` | All shell commands require explicit approval |
| `moderate` | Common commands allowed; destructive commands blocked |
| `loose` | Most commands allowed (not recommended for production) |

### Additional Security Controls

- **Blocked commands** -- configurable list of forbidden shell commands (defaults: `rm -rf /`, `dd if=/dev/zero`, `mkfs`, fork bombs)
- **Filesystem isolation** -- each agent VM has its own persistent disk at `/var/vcap/store/openclaw`; read-only paths are configurable
- **Shell timeout** -- maximum execution time for shell commands (default: 300s)
- **Outbound network blocking** -- optional firewall that allows only LLM endpoint traffic
- **Skills security scanning** -- the registry scans skills for outbound HTTP calls, fetch/axios/curl patterns, and data exfiltration indicators on every sync
- **Skills quarantine** -- allowlist-only mode (default) ensures only operator-approved skills are available to agents
- **Per-instance SSO** -- each agent VM runs its own `oauth2-proxy` with instance-scoped redirect URIs, session cookies, and email domain restrictions

### Credential Management

- Gateway tokens and node seeds are generated with crypto-secure random
- Ed25519 keypairs are derived from seeds (no key storage required)
- Secrets are managed via BOSH CredHub integration
- Token rotation is available via the broker update operation (`cf update-service`)

## Development

### Directory Structure

```
bosh-openclaw/
├── config/
│   ├── blobs.yml                 # Blob references
│   └── final.yml                 # Release name and blobstore config
├── jobs/
│   ├── openclaw-agent/           # Core agent job (runtime, WebChat, gateway)
│   ├── openclaw-broker/          # Open Service Broker API (Go)
│   ├── openclaw-registry/        # Skills registry with ClawHub sync
│   ├── openclaw-admin-dashboard/ # Operator fleet monitoring UI
│   ├── openclaw-sso-proxy/       # Per-instance oauth2-proxy
│   ├── route-registrar/          # Colocated route registration
│   ├── register-broker/          # Post-deploy errand
│   ├── deregister-broker/        # Pre-delete errand
│   ├── smoke-tests/              # Post-deploy validation errand
│   └── upgrade-agents/           # Rolling upgrade errand
├── packages/
│   ├── node22/                   # Node.js 22 runtime
│   ├── openclaw/                 # OpenClaw npm package + skills bundle
│   ├── openclaw-broker/          # Go service broker binary
│   ├── openclaw-admin-dashboard/ # Dashboard web app
│   ├── oauth2-proxy/             # SSO proxy binary
│   └── golang-1.22/              # Go compiler for broker build
├── src/
│   └── openclaw-broker/          # Go source for service broker
│       ├── broker/               # OSB API handlers (catalog, provision, bind)
│       ├── bosh/                 # BOSH Director API client
│       └── security/             # Token generation, seed derivation, policy
├── manifests/
│   ├── openclaw.yml              # Full multi-VM deployment manifest
│   └── openclaw-lite.yml         # Minimal broker-only manifest
├── scripts/
│   ├── add-blob.sh               # Download and add OpenClaw + Node.js blobs
│   ├── build-tile.sh             # Production tile build
│   └── build-tile-dev.sh         # Dev tile build (offline)
├── tile.yml                      # Ops Manager Tile Generator config
├── resources/
│   └── icon.png                  # Tile icon
└── .github/workflows/
    └── ci.yml                    # CI/CD pipeline
```

### Running Broker Unit Tests

```bash
cd src/openclaw-broker && go test ./...
```

Requires Go 1.22+.

### CI/CD Pipeline

The `.github/workflows/ci.yml` pipeline runs on push to `main` and on tags:

1. **test** -- runs `go test ./...` on the broker source
2. **build-release** -- installs the BOSH CLI and runs `bosh create-release`
3. **build-tile** -- installs `tile-generator` and runs `tile build` to produce a `.pivotal` file
4. **release** -- on version tags (`v*`), publishes the `.pivotal` tile to GitHub Releases

### Key Scripts

| Script | Purpose |
|---|---|
| `scripts/add-blob.sh <version>` | Downloads OpenClaw npm tarball and Node.js source, adds them as BOSH blobs |
| `scripts/build-tile.sh` | Creates BOSH release and builds the Ops Manager tile |
| `scripts/build-tile-dev.sh` | Dev tile build (no network dependencies) |

## Jobs Reference

| Job | Type | Description |
|---|---|---|
| `openclaw-agent` | Service | Dedicated OpenClaw runtime with WebChat UI, Gateway WebSocket, and execution node on an isolated VM |
| `openclaw-broker` | Service | Open Service Broker API (Go). Provisions and deprovisions dedicated agent VMs via the BOSH Director |
| `openclaw-registry` | Service | Curated skills repository. Hosts approved skills, syncs from ClawHub, enforces allowlist with security scanning |
| `openclaw-admin-dashboard` | Service | Operator web UI for fleet monitoring: instance health, usage metrics, version compliance |
| `route-registrar` | Colocated | Registers per-instance routes with the CF Go Router via NATS. Colocated on agent VMs, broker, and dashboard |
| `openclaw-sso-proxy` | Colocated | Per-instance oauth2-proxy for WebChat SSO. Authenticates users via p-identity/OIDC before reaching the WebChat UI |
| `register-broker` | Errand | Post-deploy: registers the service broker with Cloud Foundry and enables service access |
| `deregister-broker` | Errand | Pre-delete: deregisters the service broker from Cloud Foundry |
| `smoke-tests` | Errand | Post-deploy: provisions a test agent, verifies WebChat route accessibility and LLM connectivity |
| `upgrade-agents` | Errand | Rolling upgrade of all provisioned agent VMs to a target OpenClaw version with canary support |

## Packages Reference

| Package | Dependencies | Description |
|---|---|---|
| `node22` | -- | Node.js 22 runtime compiled from source |
| `openclaw` | `node22` | OpenClaw npm package and curated skills bundle |
| `openclaw-broker` | `golang-1.22` | Go service broker binary implementing OSB API v2.16 |
| `openclaw-admin-dashboard` | `node22` | Admin dashboard web application |
| `oauth2-proxy` | -- | Pre-built oauth2-proxy binary for per-instance SSO |
| `golang-1.22` | -- | Go 1.22 compiler toolchain (build-time dependency for broker) |

## Troubleshooting

### Agent provisioning times out

**Symptom:** `cf service my-agent` shows `create in progress` for more than 5 minutes.

**Diagnosis:**
```bash
# Check broker logs
bosh -d openclaw logs openclaw-broker

# Check if the agent deployment was created
bosh deployments | grep openclaw-agent

# Check agent VM status
bosh -d openclaw-agent-<instance-id> vms
```

**Common causes:**
- BOSH Director unreachable from broker VM (check `director_url` and network connectivity)
- Stemcell not uploaded (`bosh stemcells` should show ubuntu-jammy)
- Agent network not configured in cloud config (`bosh cloud-config`)
- Insufficient IaaS quota for new VMs

### WebChat UI not accessible

**Symptom:** Agent is provisioned but the WebChat URL returns 404 or connection refused.

**Diagnosis:**
```bash
# Check route registration
bosh -d openclaw-agent-<instance-id> ssh agent -c \
  "cat /var/vcap/jobs/route-registrar/config/registrar.json"

# Verify NATS connectivity
bosh -d openclaw-agent-<instance-id> logs route-registrar

# Check Go Router routes
cf curl /routing/v1/routes | grep openclaw
```

**Common causes:**
- NATS credentials incorrect (check `nats_machines`, `nats_user`, `nats_password`)
- Apps domain mismatch between tile config and actual CF domain
- Agent process crashed before route registration completed (check monit status)

### SSO redirect loop

**Symptom:** Accessing the WebChat URL results in an infinite redirect loop through the identity provider.

**Diagnosis:**
```bash
bosh -d openclaw-agent-<instance-id> ssh agent -c \
  "cat /var/vcap/jobs/openclaw-sso-proxy/config/oauth2-proxy.cfg"
```

**Common causes:**
- `p-identity` service instance not provisioned for this agent
- Cookie secret mismatch (regenerate via `cf update-service`)
- OIDC issuer URL incorrect or unreachable from agent VM
- Allowed email domains too restrictive

### LLM connectivity failure

**Symptom:** Agent is running but returns errors when prompted.

**Diagnosis:**
```bash
bosh -d openclaw-agent-<instance-id> ssh agent -c \
  "curl -s https://<genai-endpoint>/v1/models"
```

**Common causes:**
- GenAI endpoint unreachable (check network/firewall rules, especially if `block_outbound_network` is enabled)
- API key invalid or expired
- Model name not available on the configured provider

### Skills not loading

**Symptom:** Agent reports no skills available despite registry being configured.

**Diagnosis:**
```bash
# Check registry health
bosh -d openclaw logs openclaw-registry

# Verify sync status
bosh -d openclaw ssh openclaw-registry -c \
  "ls /var/vcap/store/openclaw-registry/"
```

**Common causes:**
- Registry VM has no persistent disk (check `persistent_disk_type` in manifest)
- ClawHub sync failed (check outbound network from registry VM)
- Skills allowlist is empty in tile configuration
- Security scan quarantined a skill (check registry logs for scan results)

### Smoke tests fail

**Symptom:** Post-deploy errand `smoke-tests` fails.

**Diagnosis:**
```bash
bosh -d openclaw run-errand smoke-tests --keep-alive
bosh -d openclaw logs smoke-tests
```

**Common causes:**
- CF API URL or admin credentials incorrect
- Smoke test org/space does not exist (created automatically, but CF API access may be blocked)
- Provisioning timeout too short for the IaaS (increase `smoke_tests.timeout_seconds`)
- Apps domain not matching the domain configured in the tile

## License

Apache License 2.0
