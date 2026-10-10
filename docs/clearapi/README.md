# Clear API public documentation

This directory is the version-controlled source for the Clear API public documentation. It is not, by itself, a published documentation centre.

- Chinese is the default technical-documentation language: [`zh/`](zh/).
- English technical documentation is available in [`en/`](en/).
- No legal documents are included in this delivery.

## Website integration plan

The website can publish these Markdown files through its existing content pipeline, or render them from a checked-out documentation package. The future integration should provide a `/docs` public navigation entry, a language switch that preserves the current document where translated, verification-level labels, and model links to `/v1/models` rather than a hard-coded catalogue. Shared navigation and frontend routes are intentionally unchanged; website integration is a later controlled task. This package does not mean the site already publishes these pages or that an account pool is publicly available.

## Verification levels

- **Source route**: a route and handler exist in this repository.
- **Mock contract**: an independent test covers route registration and/or response shape with local fixtures.
- **Production/provider verified**: a non-mocked provider call was completed.
- **Not verified**: source may exist, but no provider acceptance has been performed.

This branch contains no provider acceptance and makes no paid provider requests.
