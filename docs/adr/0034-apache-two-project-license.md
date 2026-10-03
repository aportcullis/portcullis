# ADR-0034: Apache License 2.0 for Portcullis

- Status: Accepted
- Date: 2026-10-03

## Context

The owner selected Apache License 2.0 while preparing to invite users and contributors. The PRD, README, and contribution guide previously left the project license unresolved. MIT and Apache 2.0 were considered; both permit commercial use and redistribution, while Apache 2.0 explicitly defines a contributor patent grant and contribution terms.

## Decision

License Portcullis under the unmodified official Apache License 2.0 text in root LICENSE. Add project attribution in NOTICE, expose the license from README, and use the SPDX identifier Apache-2.0 in project package metadata. Update both PRD translations together.

Explain intentional contributions using section 5 of the license. Preserve third-party licenses and notices; the project license does not relicense dependencies or separately licensed vendored code. Do not introduce a separate CLA/DCO procedure, copyright assignment, new trademark policy, paid-feature boundary, or foundation affiliation through this decision.

## Consequences

Users and contributors have a stated project license rather than a pending publication decision. Future distribution must preserve applicable license and attribution material. Separate contributor agreements, branding policy, and commercial-feature decisions remain with the owner. Product functionality and release qualification are unchanged.

## Primary sources (verified 2026-10-03)

- [Official Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0.txt), particularly sections 3–6.
- [Apache licensing FAQ](https://www.apache.org/foundation/license-faq.html), applying the license to non-ASF software.
