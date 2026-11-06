# Zebu perimeter security plan

Status: design agreed after review; implementation and live verification remain outstanding. November 5, 2026. No infrastructure changes have been applied by this plan.

## Agreed design

Public websites use official `cloudflared` connectors with explicit, version-controlled hostname-to-Service mappings. Remove Traefik and public web ingress after verifying that the applications do not depend on unreplaced Traefik features. Kubernetes API access uses Cloudflare One Client (WARP) private networking for both administrators and GitHub deployment runners.

```text
Public websites:
Visitors → Cloudflare HTTPS/cache/WAF → website tunnel
         → website cloudflared Deployment → ClusterIP Services → applications

Private administration and deployment:
Administrator device / GitHub runner with WARP → Cloudflare private routing
         → administrative tunnel → administrative cloudflared Deployment
         → private AKS API

Emergency administration:
Administrator → Azure management plane → temporarily enable public AKS API
              restricted to administrator's current public IP
```

Both connector Deployments run inside AKS. Website and administrative tunnels have separate credentials and network permissions. There is no dedicated connector VM and no Tailscale/Funnel dependency in the selected design.

Cloudflare outages mean website downtime. There is no alternate public website ingress, including during an outage. Temporary public access is an emergency procedure for the Kubernetes API only.

This is the first layer of defense. Cloudflare terminates visitor connections, but can still forward hostile HTTP requests. Applications and cloudflared still require updates, authentication, and appropriately restricted permissions. This design does not claim protection from every protocol or application CVE.

## Current evidence and prerequisites

The checked-in [AKS configuration](../infra/zebu.json) describes a Standard load balancer, Azure CNI overlay, and Cilium. It is not a live exposure audit. Before implementation:

- Inventory Azure public IPs, load balancer rules, NSGs, public node IPs, Kubernetes LoadBalancer/NodePort Services, ingress routes, and dashboards. Verify the assumption that web ingress and the API are the only public entry points.
- Inspect the live AKS API access profile. Confirm private API migration prerequisites and support for restoring public access through Azure management operations. API Server VNet Integration supports toggling private mode, but zebu's compatibility is not yet verified.
- Inventory Traefik middleware, certificate handling, authentication, redirects, rewrites, header manipulation, rate limits, and non-HTTP traffic. Replace required behavior explicitly before removal.
- Identify all API clients, including deployment automation and monitoring.
- Prove WARP enrollment, private DNS, routing, and access policy on a GitHub-hosted runner before disabling public API access.

References: [AKS API Server VNet Integration](https://learn.microsoft.com/en-us/azure/aks/api-server-vnet-integration), [private AKS clusters](https://learn.microsoft.com/en-us/azure/aks/private-clusters).

## Public websites

Cloudflared opens outbound connections to Cloudflare and forwards hostname-matched requests directly to Kubernetes Service DNS names. The final application Services are ClusterIP Services; no public web load balancer, NodePort, or ingress controller is required. Kubernetes Services distribute traffic to application pods.

Example routing fragment for a locally managed tunnel; names and ports are placeholders:

```yaml
ingress:
  - hostname: careme.example.com
    service: http://careme.default.svc.cluster.local:8080
  - hostname: another.example.com
    service: http://another.default.svc.cluster.local:80
  - service: http_status:404
```

Associate each public DNS hostname with the named tunnel. Manage routing through reviewed configuration, not an additional community ingress controller. Plain cloudflared does not watch Kubernetes Ingress resources.

- Run at least two website connector replicas spread across nodes and verify connector failover.
- Restrict website connectors to published application ports and required DNS/Cloudflare connectivity. They should not have an administrative network route to the Kubernetes API.
- Keep website tunnel credentials separate from administrative credentials. Do not provide unneeded Kubernetes API tokens or Cloudflare configuration-editing credentials to connector pods.
- Test hostname handling, login redirects, uploads, streaming, WebSockets, and cache behavior where used. Verify unknown hostnames do not reach an application.
- The tunnel encrypts traffic to cloudflared. The example's final hop is HTTP inside the cluster; determine whether HTTPS with certificate verification is needed for that hop before rollout.
- Remove public ingress only after the tunnel path passes verification. Remove unused Traefik resources afterward.

Preserve AKS outbound connectivity. The existing Azure load balancer also provides cluster egress; removing a public ingress Service does not mean deleting the entire Azure load balancer.

References: [Tunnel routing configuration](https://developers.cloudflare.com/tunnel/features/locally-managed-tunnels/configuration-file/), [Kubernetes connector deployment](https://developers.cloudflare.com/tunnel/guides/kubernetes/).

## Private API access through WARP

Use organization-enrolled WARP and Cloudflare private networking, not a public website route for the API. Configure only the required API destination route, split-tunnel settings, and private DNS resolution. Keep the API's proper hostname and normal TLS certificate verification.

The administrative cloudflared Deployment uses its own tunnel and credentials. Limit its network reach to the API and necessary DNS/Cloudflare dependencies. Use redundant connectors and test loss of a replica. An in-cluster connector failure may interrupt both administration and deployment; the accepted recovery path is through Azure management operations below.

Human and automation access use separate identities and policies. WARP provides network connectivity; existing Kubernetes/Entra authentication and RBAC still determine API permissions. Explicitly allow the intended administrator and automation identities to the API port, normally TCP 443, rather than granting broad VNet access. Final identity-provider, MFA, and credential details remain implementation prerequisites.

Test kubectl queries, logs, exec, port-forward, and deployment operations. Public website visitors do not need WARP. Cloudflare access and device-policy features have their own plan limits; free website tunneling does not imply all Zero Trust features are free.

References: [private application access](https://developers.cloudflare.com/cloudflare-one/access-controls/applications/non-http/self-hosted-private-app/), [private DNS](https://developers.cloudflare.com/learning-paths/secure-internet-traffic/configure-device-agent/private-dns/).

## GitHub deployment continuity

The current [GitHub workflow](../.github/workflows/go.yml) deploys caremetest from a GitHub-hosted Ubuntu runner after successful image publishing on pushes to master. It reads a stored kubeconfig and invokes [the deployment script](../deploy/deploy.sh). The repository does not establish the kubeconfig's live credential type or endpoint. No private-network connection is currently configured in that workflow.

Preserve automatic test deployment for every master commit that passes the existing pipeline gates. Branch commits must not compete to overwrite the shared test deployment. Production deployment triggered by releases is a future option, not part of this phase.

Add a private connection before the existing deployment step:

1. Enroll/connect the runner using headless WARP with dedicated automation credentials.
2. Verify private DNS and API connectivity, then run the existing Kubernetes deployment with its separate Kubernetes identity.
3. Disconnect and clean up ephemeral device registrations, including on job failure.

Cloudflare documents headless Linux enrollment using a service token, but this specific GitHub runner integration remains unproven. Test unattended enrollment, policy restrictions, credential handling, cleanup, and deployment end to end. Protect credentials from untrusted pull-request jobs. Do not reopen the API automatically when a deployment cannot connect; fail the deployment and retain the existing running application.

Reference: [headless Cloudflare One Client deployment](https://developers.cloudflare.com/cloudflare-one/tutorials/deploy-client-headless-linux/).

## Connector updates

Pin cloudflared image versions and ideally automate PRs proposing updates. Deploy infrastructure version changes after approval and merge, retaining the previous known-good version for rollback. Test the rollout without losing all connector replicas. This review step applies to connector updates, not normal application autodeployment.

Assign ownership for reviewing security updates, tunnel credential rotation, and failed connector/deployment alerts. Removing public listeners does not eliminate this maintenance obligation.

## Recovery: temporary restricted public API access

The agreed recovery path for tunnel/provider failures is Azure management-plane access. With a compatible AKS API Server VNet Integration configuration, public access can be restored without working kubectl or connector pods. Verify and record the exact commands against zebu before depending on this path.

1. Sign into Azure directly with WARP disconnected and confirm permission to update AKS networking.
2. Restore public API access restricted to the administrator's current public IP (/32 for IPv4), preserving required cluster connectivity. Test the update sequence so there is no unrestricted public interval.
3. Connect using existing Kubernetes credentials. Repair the connectors or wait for Cloudflare to recover. Account for Azure update time and any DNS/kubeconfig changes.
4. Verify private access using fresh administrator and automation connections, disable public API access again, and verify public connections fail.

Run a complete recovery drill before closing the public endpoint. Keep the tested runbook accessible without the cluster or Cloudflare. An existing authenticated kubectl session is not a recovery mechanism.

This procedure depends on Azure management availability and usable administrator credentials; it does not fix an unavailable AKS control plane. It restores administration only. Websites stay down during a Cloudflare outage.

References: [AKS API Server VNet Integration](https://learn.microsoft.com/en-us/azure/aks/api-server-vnet-integration), [authorized IP ranges](https://learn.microsoft.com/en-us/azure/aks/api-server-authorized-ip-ranges).

## Rollout and acceptance criteria

| Stage | Work | Acceptance criteria |
| --- | --- | --- |
| 1. Inventory | Inspect live exposure, AKS conversion compatibility, API clients, and Traefik dependencies. | All public listeners are accounted for; required application behavior and migration prerequisites are known. |
| 2. Prove private access | Deploy separate administrative connectors and configure WARP, DNS, and access policies. | Authorized administrators can use queries, logs, exec, and port-forward; unauthorized identities cannot reach the API. |
| 3. Prove GitHub deployment | Add headless WARP to the existing deployment job. | A master commit deploys caremetest unattended through the private path; failed connection setup fails safely; runner registrations are cleaned up. |
| 4. Prove recovery | Exercise temporary IP-restricted public API access through Azure with connectors unavailable. | Administration is restored without running connector pods; private access is recovered and public access is closed again. |
| 5. Migrate websites | Configure hostname routes and verify applications, then remove public web ingress and Traefik. | Sites work through Cloudflare, connector failover works, unknown hosts are rejected, and direct-origin public access is unavailable. |
| 6. Close public API | Disable public API access after the previous checks pass. | Administrator and GitHub private access still work; public API connections fail; cluster health remains normal. |

Record previous configurations before changes. During migration, any temporary rollback to the old public web ingress must include Cloudflare-only network restrictions. Such a migration rollback is not a standing website disaster-recovery path. After migration, the final design has no alternate public website ingress.

## Alternatives considered

### Public load balancer restricted to Cloudflare IPs

AKS supports spec.loadBalancerSourceRanges on a LoadBalancer Service. This can reject connections before Traefik parses TLS/HTTP, while retaining the existing architecture. Full (strict) origin TLS and custom zone-level Authenticated Origin Pulls can add protection. The shared Cloudflare IP ranges and default origin-pull certificate do not identify this particular Cloudflare zone.

The maintenance tradeoff is ongoing synchronization. Cloudflare says its ranges change infrequently, but a static list can block new legitimate sources and retain removed ones. A safe synchronizer would validate official lists, reconcile the owning Service configuration, retain the last known-good list on failure, never fail open, and alert on staleness. Even automation cannot eliminate the gap between a source-range change and Azure rule propagation.

Not selected because Tunnel removes that origin-source allowlist and public listener. Tunnel instead requires operating and updating connectors.

References: [AKS load balancer restrictions](https://learn.microsoft.com/en-us/azure/aks/configure-load-balancer-standard), [Cloudflare IP ranges](https://developers.cloudflare.com/fundamentals/concepts/cloudflare-ip-addresses/), [zone-level origin pulls](https://developers.cloudflare.com/ssl/origin-configuration/authenticated-origin-pull/set-up/zone-level/).

### Tailscale private API access and public Funnel

Tailscale subnet routing or its Kubernetes API proxy could replace WARP for private administration. An external subnet-router VM would provide access independent of cluster pods, at the cost of another maintained host. WARP with in-cluster connectors was selected to consolidate connectivity, with Azure-based recovery accepted instead.

Tailscale's official operator can create public HTTPS Funnels from Ingress resources. Cloudflare could potentially proxy those endpoints with suitable origin hostname/SNI handling, but the public ts.net endpoint remains a bypass around Cloudflare. Funnel with filtering inside Traefik also fails the requirement to reject unwanted connections before Traefik processes them. Funnel is not selected for public services.

References: [Tailscale API access](https://tailscale.com/docs/kubernetes-operator/api-server-access), [operator Funnel ingress](https://tailscale.com/docs/kubernetes-operator/ingress/expose-workload-to-internet), [Cloudflare Origin Rules](https://developers.cloudflare.com/rules/origin-rules/).

### Kubernetes-native Cloudflare controllers

[STRRL's controller](https://tunnel.strrl.dev/) watches standard Ingress resources and manages tunnels, connectors, and DNS. [Lexfrei's controller](https://github.com/lexfrei/cloudflare-tunnel-gateway-controller/) uses Gateway API. Cloudflare's [older controller](https://github.com/cloudflare/cloudflare-ingress-controller) was archived on November 7, 2023.

Community controllers were considered but not selected. Start with official cloudflared and explicit mappings to avoid another controller and its management credentials. Reconsider if mapping maintenance becomes burdensome; assess project maintenance, permissions, exposure controls, and whether its traffic path adds another HTTP proxy.

## Cost and deferred work

As checked November 5, 2026, ordinary website-origin Cloudflare Tunnel use is available without an additional Tunnel charge. Paid Cloudflare features and Azure compute/egress remain separate. Review [content restrictions and published-application limits](https://developers.cloudflare.com/tunnel/concepts/routing/), especially for video or large-file delivery, and confirm the chosen Zero Trust features and pricing before deployment. See Cloudflare's [free Tunnel announcement](https://blog.cloudflare.com/tunnel-for-everyone).

Deferred explicitly:

- Admission enforcement preventing future public LoadBalancer/NodePort Services. Existing exposure is audited and removed, but a later privileged deployment could recreate public ingress; this remains a known gap.
- Production release-triggered deployment.
- Broader application security and workload isolation beyond the connector restrictions required here.

Completion means the selected design has passed the live acceptance checks. Agreement on this document is not evidence that the cluster is already secured.
