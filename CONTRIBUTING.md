# Contributing

Thank you for wanting to improve Phoedox! Phoedox consists of several repositories:
[phoedox](https://github.com/phoedox/phoedox) (server + web), [phoedox-sdk](https://github.com/phoedox/phoedox-sdk),
[phoedox-plugins](https://github.com/phoedox/phoedox-plugins), [phoedox-agent](https://github.com/phoedox/phoedox-agent),
[phoedox-store](https://github.com/phoedox/phoedox-store) and [phoedox-app](https://github.com/phoedox/phoedox-app).
The fundamental decisions are kept in Phoedox itself (project `phoedox`, public once `docs.phoedox.com` is live); until then, the state in the Git history of [`phoedox/docs/decisions.md`](https://github.com/phoedox/phoedox/blob/main/docs/decisions.md) applies. Please describe new fundamental decisions in the pull request – they will be adopted as ADRs.

## Conduct

Our [Code of Conduct](CODE_OF_CONDUCT.md) applies. Please report violations to admin@coph.dev.

## Before you start

- For larger changes, please open an issue first and agree on the approach.
- Do **not** report security vulnerabilities as an issue; follow [SECURITY.md](SECURITY.md) instead.

## Workflow

1. Fork, branch from `main` (`feat/…`, `fix/…`).
2. Changes with tests. The commands are in the repo's README or `CLAUDE.md`.
3. Commits follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) (`feat:`, `fix:`, `docs:` …) – versions and the changelog are generated from them.
4. Pull request with the template filled in.

## Contributor License Agreement (CLA)

Contributions to this repository require a [CLA](https://github.com/phoedox/phoedox/blob/main/CLA.md).
With it, you grant the project the rights to also offer your contribution under other licenses
(e.g. an enterprise license alongside the AGPL). You keep your copyright.
On your first pull request, the CLA bot asks you to comment the following sentence in the PR:

> I have read the CLA Document and I hereby sign the CLA

## Style

- Code, identifiers and comments in English; UI texts in German and English via i18n.
- Icons only from Lucide.
- No secrets, tokens or real customer data in code, tests or screenshots.
