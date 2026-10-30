# Spike S2 — Is a Read-Only `discovery` Credential Enough for a Full Inventory?

- **Date:** 2026-10-30
- **Related:** ADR-0005 (two-tier credentials), roadmap §3.4 (provider credentials), §3.6 (inventory sync)
- **Outcome:** 🟡 Desk result, lab confirmation pending — **Proxmox: yes** with `PVEAuditor` on PVE 9. **OpenStack: no** with the upstream default policies; a read-only role needs a policy override (fallback below).

## Question

ADR-0005 plans one `discovery` credential per provider for inventory sync: an OpenStack `reader` (plus Placement) and a Proxmox `PVEAuditor` token. Can that credential see every resource SEMAR lists, across all projects and pools, without write access?

## Method

Every API call made by the read-only drivers (`internal/driver/openstack`, `internal/driver/proxmox`) was mapped to the policy rule or privilege that guards it. Upstream defaults were checked in the current Nova and Placement policy references and in the Proxmox `pve-access-control` and `qemu-server` sources. The OpenStack lab result from 2026-10-18 is used as evidence where it applies.

## Findings — OpenStack

### Calls and default policies

| Call | What SEMAR needs | Upstream default (scope `project`) |
|---|---|---|
| Nova `GET /servers/detail?all_tenants=1` | every project's servers | `os_compute_api:servers:detail:get_all_tenants` = `context_is_admin` |
| Nova `GET /os-hypervisors/detail` | hypervisors | `os_compute_api:os-hypervisors:list-detail` = `context_is_admin` |
| Placement resource providers, inventories, usages, traits | physical capacity | `admin_or_service_api` (`role:admin or role:service`) |
| Cinder `GET /volumes/detail?all_tenants=1` | every project's volumes | admin only |
| Neutron networks, subnets, security groups | every project's objects | a non-admin sees its own project plus shared/external objects |
| Glance `GET /v2/images` | every image | a non-admin sees public, shared and its own project's images |

### What this means

1. **A `reader` is not enough.** Every cross-project listing above is admin-only by default; a reader sees one project.
2. **System scope no longer helps.** Nova and Placement policies accept project-scoped tokens only, and `enforce_scope` / `enforce_new_defaults` are on by default in current releases. The driver's password mode asks for a **system-scoped** token (`scope.system.all`) and will be refused by Nova and Placement on such clouds.
3. **The lab confirms the shape.** The lab credential is a project-scoped application credential. Hypervisors and Placement capacity were readable, so it carries an admin-level role. VMs, volumes and security groups came from one project only, because the driver sends `all_tenants=1` only in password mode.

### Fallback (proposed)

1. **Dedicated read-only role via policy override.** Create a role (for example `semar-discovery`) and a policy file for Nova, Placement, Cinder, Neutron and Glance that lets it run the read-only rules above, and nothing else. The credential is project-scoped (any project), and the driver sends `all_tenants=1`. Kolla-Ansible reads custom policy files per service; openstack-helm based deployments (such as Atmosphere) take them as chart values.
2. **Where a policy override is not allowed:** a project-scoped credential with `admin` in a dedicated project, accepted as a documented risk (admin is cloud-wide in OpenStack, so it can also mutate). The credential stays `discovery` in SEMAR and is never used for writes.
3. **Driver change needed for both:** cross-project listing must be decided by configuration, not by the auth method. Add a project-scoped password mode (`project_name` + `project_domain_name`) and an explicit "list all projects" setting; drop or keep the system-scope mode only behind a capability check.

## Findings — Proxmox VE

| Call | Privilege | In `PVEAuditor` (PVE 9) |
|---|---|---|
| `GET /cluster/resources`, `/cluster/status`, `/nodes/{node}/status` | `Sys.Audit` / `VM.Audit` / `Datastore.Audit` / `Pool.Audit` | ✅ |
| `GET /nodes/{node}/qemu/{vmid}/config` (disks, arch) | `VM.Audit` | ✅ |
| `GET /nodes/{node}/network` (bridges) | `Sys.Audit` | ✅ |
| `GET /cluster/sdn/vnets` | `SDN.Audit` | ✅ |
| `GET /nodes/{node}/qemu/{vmid}/agent/network-get-interfaces` (VM IPs) | `VM.GuestAgent.Audit` (PVE 9); `VM.Monitor` (PVE 8) | ✅ PVE 9 · ❌ PVE 8 |

- PVE 9 dropped `VM.Monitor` and split guest-agent access into `VM.GuestAgent.*`. `PVEAuditor` is built from every privilege group's `audit` list and therefore includes `VM.GuestAgent.Audit`.
- On PVE 8, `PVEAuditor` has no `VM.Monitor`; the driver then shows "IP unknown" (guest-agent errors are already treated as "no addresses"). Adding `VM.Monitor` would also grant HMP monitor access, so it is not recommended.
- The token must be created with privilege separation and its own `PVEAuditor` ACL on `/`, because a token's effective rights are the intersection of the user's and the token's.

## Decisions (to be agreed)

1. Proxmox `discovery` = API token with `PVEAuditor` on `/`; PVE 9 is the baseline for VM IPs.
2. OpenStack `discovery` = project-scoped credential with a dedicated read-only role and a policy override (fallback 1). Fallback 2 only where the cloud owner refuses a policy override.
3. Driver: project-scoped password auth + explicit all-projects setting (Phase 1 change, needed for the "≥ 50 VMs across projects" exit criterion).

## Open Items

- Write the policy override for the lab cloud, create the role and credential, and compare SEMAR's counts with an admin listing (servers, volumes, networks, security groups, images).
- Confirm Glance and Neutron rules for the custom role on the lab's release; add any missing rule to the override.
- Run the Proxmox inventory with a `PVEAuditor` token on the lab and record the PVE version; check VM IPs.
- Add the final role/ACL lists to a runbook.
