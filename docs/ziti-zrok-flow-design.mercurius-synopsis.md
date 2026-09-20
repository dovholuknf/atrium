---
session_id: s_1HzKPntGxCY5
opened_at: 2026-09-20T03:24:59Z
closed_at: 2026-09-20T03:27:58Z
round_count: 1
reviewer:
  name: codex
  impl: codex
  model: gpt-5.5
review_context_present: true
review_focus_present: true
---

# Session synopsis

## Summary

Closed 1 round with reviewer 'codex'. Latest verdict: 'needs_changes'. Decisions across all rounds: 4 fixed. 1 advisory note recorded across the arc.

## Round outcomes

| round | opened_at | verdict | concerns | questions | advisory | decisions | notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 01 | 2026-09-20T03:25:09Z | needs_changes | 3 | 0 | 1 | 4 fixed | yes |

## Round detail

### Round 01

- Opened: 2026-09-20T03:25:09Z
- Verdict: needs_changes
- Log: D:\git\github\dovholuknf\atrium\.mercurius\s_1HzKPntGxCY5\round-01\_round.md
- Notes: D:\git\github\dovholuknf\atrium\.mercurius\s_1HzKPntGxCY5\round-01\_notes.md

**Reviewer summary:** I would not build from this document as-is. The transport split and zrok-public refusal are directionally solid, but the board authentication contract and OpenZiti provisioning boundary are underspecified in ways that normal compile/runtime feedback will not catch until the remote phone or fabric deployment moment. Tightening those few rules should make the design buildable without expanding scope into production hardening.

**Concerns**

- **C1** (major, 'ziti-zrok-flow-design.md / Board exposure: Shared across every transport; Rules that fall out and must be enforced'): The document contradicts itself on whether OIDC login is required for all board exposure transports or only for zrok public — One section says the board login covers every exposure transport and guards all remote paths, while the enforceable rules only refuse zrok public without OIDC. Two implementers could validly produce incompatible behavior: one requiring OIDC for zrok private/OpenZiti board exposure, another allowing overlay reachability alone. That changes the security and setup contract without causing an obvious implementation failure — _suggestion_: State the rule explicitly in one sentence: either OIDC is mandatory for every non-loopback board exposure, or it is mandatory only for zrok public and optional for private/OpenZiti exposures when overlay reachability is accepted as the gate
- **C2** (major, 'ziti-zrok-flow-design.md / Board exposure; The two end-state flows / Publishing the hub board to a phone'): The OIDC callback/base-URL contract for exposed board transports is unspecified — The design requires zrok public to have OIDC before it can start, allows multiple exposure transports concurrently, and says one login configuration covers all of them. OIDC providers generally require exact redirect URIs, but the document does not define how atrium determines, persists, displays, or validates the external callback origin for zrok reserved URLs and OpenZiti service addresses. This can pass local testing and fail only when the phone path attempts login — _suggestion_: Add a small rule that each enabled board exposure has a known external origin/callback URL, atrium displays the exact redirect URI to register with the provider, and public zrok cannot be saved until its reserved URL and corresponding callback URI are known
- **C3** (major, 'ziti-zrok-flow-design.md / Credential posture, corrected; Board exposure; The room link: `atrium2 join` with a transport'): The OpenZiti board and room-link flows omit the required pre-provisioned service and policy contract even though atrium explicitly will not administer the network — The flow asks the operator only for an identity/JWT and service name, but if atrium creates no Ziti services or policies, an external admin must already have created the service and authorized the hub identity to bind and client identities to dial. Without that stated precondition, implementation can look complete while real OpenZiti deployment fails at the first bind/dial, and different implementers may invent different assumptions around the existing “what can I host?” capability query — _suggestion_: Add an OpenZiti precondition: the named service and bind/dial policies must already exist; the panel/join flow should use the existing capability query or startup preflight to refuse a service the identity is not authorized to bind or dial

**Advisory notes**

- **A1** ('ziti-zrok-flow-design.md / Board exposure: the "expose the board" panel'): The board OpenZiti path mentions only a JWT or path to a JWT, while the credential posture and room join flow both allow an already-enrolled identity file — This is likely easy for an implementer to reconcile from nearby prose, but adding the same `.json` identity-file option to the board panel would avoid needless asymmetry for operators who already have identities — _suggestion_: Add “or an enrolled identity `.json` file” to the OpenZiti board exposure input description

**Decisions**

- **fixed** (C1): Added a rule: OIDC is mandatory only for zrok public; for zrok private and openziti the overlay is the gate and login is optional. Resolves the shared-login vs enforceable-rules contradiction.
- **fixed** (C2): Added external-origin/callback rule: atrium derives the origin per transport, displays the exact redirect URI, and refuses to save a zrok public share until its reserved URL and callback URI are known.
- **fixed** (C3): Added OpenZiti precondition: service and bind/dial policies must pre-exist; atrium uses the capability query and start pre-flight to refuse an unauthorised bind/dial rather than failing at first bind.
- **fixed** (A1): Board openziti input now also accepts an already-enrolled identity .json file, matching the room-link join inputs.

