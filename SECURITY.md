# Security policy

Portcullis is in development alpha. Security fixes currently target the latest code on `main`; after a release branch is cut, patches for that minor release belong on its matching `release/vX.Y` branch.
Older commits and release lines do not receive guaranteed backports. A passing CI run is not a security audit or a production-readiness guarantee. See the [validation record](docs/milestones/m1/validation.md) and [database support matrix](docs/product/database-support.md) for verified scope.

## Report a vulnerability privately

Use GitHub's [Report a vulnerability](https://github.com/aportcullis/portcullis/security/advisories/new) form. The report is shared privately with repository maintainers through GitHub, rather than published as an issue.

If the form is unavailable, open an issue titled **Request for a private security contact** with no vulnerability details. Maintainers must arrange a private channel before you share evidence. Do not post exploit steps, affected private systems, credentials, SQL containing real data, or result rows in public issues, discussions, or pull requests. No security email address is currently designated.

Include in the private report:

- The affected application version or commit, database version, and deployment configuration relevant to the problem.
- Expected behavior, observed behavior, potential impact, and required access or prerequisites.
- Minimal reproduction steps using synthetic data; a proof of concept if available.
- Redacted logs or screenshots and any suggested mitigation.

Authentication, authorization, approval integrity, SQL execution boundaries, encryption, audit evidence, result isolation, and release or dependency compromise are all relevant areas. You do not need to prove a complete exploit to report a suspected vulnerability.

## Review and coordinated disclosure

Maintainers review reports, clarify reproduction details privately, and coordinate fixes and disclosure with the reporter. Confirmed issues may receive a GitHub security advisory with affected versions and mitigation guidance. Credit is subject to the reporter's consent. There is no guaranteed response deadline, paid bug bounty, or guaranteed CVE assignment at this stage.

Test only installations and data you own or have permission to assess. Use disposable environments and synthetic examples. Keep sensitive details private while coordinating disclosure.

## Operating guidance

Keep Portcullis on a [private network](docs/operations/recommended-architecture.md), apply least-privilege database permissions, and follow the [operations quickstart](docs/operations/pg-alpha-quickstart.md). Preserve master keys securely and use the documented TLS settings. These controls complement security fixes; they do not replace them.
