---
name: build-tile
description: Build the OpenClaw OpsMan tile correctly. Use when cutting a new release, debugging tile errors, or modifying tile configuration. Covers tile-generator format, consumes syntax, service_plan_forms, OpsMan accessors, and CI pipeline.
user-invocable: true
argument-hint: "[version]"
---

# Building the OpenClaw OpsMan Tile

You are building or debugging the OpenClaw tile for Tanzu Operations Manager.
This skill captures every hard-won lesson from iterative debugging against a real OpsMan instance.

If a version argument is provided ($ARGUMENTS), use it for the release tag.

## Architecture Overview

```
tile.yml                    # tile-generator input (SOURCE OF TRUTH)
tile/metadata/openclaw.yml  # legacy raw metadata (NO LONGER USED)
.github/workflows/ci.yml   # CI: test → build-release → build-tile → release
resources/icon.svg          # SVG icon (converted to PNG at build time)
```

The tile is built using **tile-generator** (`pip install tile-generator`), NOT hand-rolled raw metadata.
Tile-generator reads `tile.yml` and produces the `.pivotal` file with correct metadata formatting.

## tile.yml Format (tile-generator)

The tile.yml follows tile-generator format — NOT raw OpsMan metadata format.
Reference: `github.com/nkuhn-vmw/bosh-seaweedfs/blob/main/tile.yml`

Key structural rules:
- `metadata_version: 3.0` (required for modern OpsMan / TAS 4.x)
- `service_broker: true` (required for service broker tiles — enables `$self` accessors)
- `requires_product_versions` declaring CF dependency
- Jobs go under `packages: → jobs:` (NOT `job_types:`)
- Forms go under `forms:` (NOT `form_types:`)
- Errands use `lifecycle: errand` with `post_deploy: true` or `pre_delete: true`

## consumes for NATS (Route Registration)

The `route_registrar` job from the `routing` release needs NATS link consumption.

**Correct (tile-generator format):**
```yaml
templates:
  - name: route_registrar
    release: routing
    consumes:
      nats-tls:
        from: nats-tls
        deployment: (( ..cf.deployment_name ))
```

**Critical rules:**
- Use `nats-tls` (NOT `nats`) — modern TAS uses NATS over TLS
- Use `(( ..cf.deployment_name ))` accessor (NOT the literal string `cf`)
- In raw metadata, `consumes` must be a YAML **string** (`|`), not a hash — tile-generator handles this automatically
- Error `no implicit conversion of Hash into String` = consumes is a hash in raw metadata instead of a string

**Also required:** NATS TLS certificates in the job properties:
```yaml
properties:
  nats:
    tls:
      enabled: true
      client_cert: (( ..cf.properties.nats_client_cert.cert_pem ))
      client_key: (( ..cf.properties.nats_client_cert.private_key_pem ))
      ca_cert: (( ..cf.properties.nats_tls_external_cert.cert_pem ))
```

## OpsMan Property Accessors

Never create manual properties for platform credentials. Use built-in accessors:

| Need | Accessor |
|------|----------|
| BOSH Director URL | `https://(( $director.hostname )):25555` |
| BOSH Director CA | `(( $director.ca_public_key ))` |
| UAA Client ID | `(( $self.uaa_client_name ))` |
| UAA Client Secret | `(( $self.uaa_client_secret ))` |
| UAA Auth URL | `https://(( $director.hostname )):8443` |
| CF System Domain | `(( ..cf.cloud_controller.system_domain.value ))` |
| CF Apps Domain | `(( ..cf.cloud_controller.apps_domain.value ))` |
| CF Admin User | `(( ..cf.uaa.system_services_credentials.identity ))` |
| CF Admin Password | `(( ..cf.uaa.system_services_credentials.password ))` |
| CF Deployment Name | `(( ..cf.deployment_name ))` |
| Service Network | `(( $self.service_network ))` |
| Apps Domains (runtime) | `(( $runtime.apps_domains.[0] ))` |

## service_plan_forms (Dynamic On-Demand Plans)

This creates a tab in OpsMan where operators can dynamically add/remove plans.
Each plan gets a name field automatically. Additional configurable properties:

```yaml
service_plan_forms:
  - name: on_demand_agent_plans
    label: On-Demand Agent Plans
    optional: true
    properties:
      - name: vm_type
        type: vm_type_dropdown      # populated from BOSH cloud config
      - name: disk_type
        type: disk_type_dropdown    # populated from BOSH cloud config
      - name: browser_automation
        type: boolean
        default: false
```

**Requirements for service_plan_forms to work:**
- `metadata_version: 3.0`
- `service_broker: true`
- Must be built via tile-generator (does NOT work in hand-rolled raw metadata)
- Without these, OpsMan rejects with "not upgradable using this version"

Reference plans in the broker manifest:
```yaml
plans: (( .properties.on_demand_agent_plans.value ))
```

## Route Registration Pattern

Route prefixes (not full hostnames) combined with system domain:
```yaml
route_registrar:
  routes:
    - name: openclaw-broker
      port: 8080
      registration_interval: 20s
      uris:
        - (( .properties.broker_route_prefix.value )).(( ..cf.cloud_controller.system_domain.value ))
```

## Vendored Releases

The tile vendors three BOSH releases for air-gap support:

| Release | Version | Path |
|---------|---------|------|
| openclaw | (built from source) | `resources/openclaw-release.tgz` |
| bpm | 1.1.21 | `resources/bpm-release.tgz` |
| routing | 0.283.0 | `resources/routing-release.tgz` |

## CI Pipeline

The GitHub Actions CI has 4 jobs: `test → build-release → build-tile → release`

### build-release
- Creates BOSH release with `bosh create-release --force --version="${VERSION}"`
- **Must pass `--version`** — without it, BOSH produces dev releases like `0+dev.1`

### build-tile
```bash
pip install tile-generator
mkdir -p resources
cp openclaw-release.tgz resources/openclaw-release.tgz
curl -sL "https://bosh.io/d/github.com/cloudfoundry/bpm-release?v=1.1.21" -o resources/bpm-release.tgz
curl -sL "https://bosh.io/d/github.com/cloudfoundry/routing-release?v=0.283.0" -o resources/routing-release.tgz
rsvg-convert -w 256 -h 256 resources/icon.svg -o resources/icon.png
tile build "${VERSION}"
```

Tile-generator handles: SHA1 computation, icon embedding, metadata generation, .pivotal zip assembly.

### release
Triggered on `v*` tags only. Creates GitHub Release with the `.pivotal` artifact.

## Cutting a New Release

```bash
# 1. Make changes to tile.yml, jobs, packages, etc.
# 2. Commit and push
git add -A && git commit -m "description" && git push origin main
# 3. Tag and push (triggers CI)
git tag v0.0.X && git push origin v0.0.X
# 4. Wait for CI to build
# 5. Download .pivotal from GitHub Releases
# 6. Upload to OpsMan: Settings → Import a Product
```

**If upgrading from a different metadata_version:** Delete the existing product from OpsMan first.

## Common Errors

| Error | Cause | Fix |
|-------|-------|-----|
| `no implicit conversion of Hash into String` | `consumes` is a YAML hash in raw metadata | Use tile-generator (handles formatting) or make `consumes` a YAML string |
| `not upgradable using this version` | metadata_version mismatch or missing `service_broker: true` | Use 3.0 + `service_broker: true`; delete existing product if upgrading |
| `Release version 'X' doesn't exist` | `bosh create-release --force` without `--version` | Add `--version="${VERSION}"` |
| `Can't find property` | ERB `p()` for property not in manifest | Add property to job manifest or add spec default |
| `undefined method 'segments' for nil` | Stemcell version not parseable | Use `"1.0"` not `"latest"` or `"1.*"` |
| `Missing releases in product template` | Release filename mismatch | Ensure `path:` in tile.yml matches actual file in `resources/` |
| service_plan_forms not visible | Using raw metadata instead of tile-generator | Switch to tile-generator build |

## GenAI Selector with named_manifests

The LLM provider uses a `selector` with `named_manifests` for clean manifest interpolation:

```yaml
- name: genai_provider
  type: selector
  option_templates:
    - name: anthropic_direct
      select_value: "Anthropic (Direct)"
      named_manifests:
        - name: genai_fragment
          manifest: |
            provider: anthropic
            api_key: (( .properties.genai_provider.anthropic_direct.api_key.value ))
            model: (( .properties.genai_provider.anthropic_direct.model.value ))
```

Referenced in the broker job:
```yaml
genai: (( .properties.genai_provider.selected_option.parsed_manifest(genai_fragment) ))
```

## Auto-Generated Credentials

Use `simple_credentials` type for broker auth — OpsMan generates and manages these:
```yaml
- name: generated_broker_credentials
  type: simple_credentials
  configurable: false
```

Reference as `(( .properties.generated_broker_credentials.identity ))` and `.password`.
