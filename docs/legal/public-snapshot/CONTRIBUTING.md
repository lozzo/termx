# Contributing to Muxvia

This file applies to the separate public Muxvia repository created from an approved public snapshot. The private authoritative monorepo does not accept public contributions.

## License

The public project is licensed under Apache-2.0. Unless a contribution is explicitly marked `Not a Contribution`, material intentionally submitted for inclusion is provided under Apache-2.0 without additional terms.

No contributor may submit private Muxvia service code, credentials, customer data, proprietary SDK code, generated secrets, or material they do not have the right to redistribute.

## DCO

Every commit must certify the Developer Certificate of Origin 1.1 in `DCO` by including a sign-off created with:

```bash
git commit -s
```

The sign-off must use a name and email address the contributor is authorized to use:

```text
Signed-off-by: Contributor Name <contributor@example.com>
```

The project uses DCO sign-off rather than a separate contributor license agreement. A future CLA requires an explicit governance decision and cannot be introduced silently.

## Pull Requests

- Keep changes within the public architecture and security boundaries documented in `docs/remote-platform/`.
- Add focused tests for behavior changes and keep generated files reproducible from checked-in sources.
- Preserve third-party copyright, attribution, license, and modification notices.
- Do not add dependencies with unknown, noncommercial, source-available, reciprocal, or restricted terms without an explicit license review.
- Report suspected vulnerabilities through the hosting provider's private security-reporting channel rather than a public issue.

Maintainers may reject or remove a contribution when authorship, provenance, DCO sign-off, license compatibility, or security ownership cannot be established.
