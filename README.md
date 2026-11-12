# logos-core

Core of Logos, a modular team operations platform: the server every module
(channels, factories) and every operator talks to. It keeps operations, the
wiki, credentials and the rest in MongoDB and Redis; registers modules over
RabbitMQ; serves minions' traffic through the channels; builds minions
through the factories; and serves the web UI. What core does and the
contracts it keeps with modules and minions are on the docs site:
https://logoscore.github.io/logos-docs/.

## Layout

| | |
|---|---|
| `core/` | The Go server (`github.com/logoscore/logos-core/core`): REST and GraphQL under `/api/v1`, the channel and factory endpoints under `/api/channel` and `/api/factory`, everything minions need. |
| `frontend/` | The web UI, a React single-page app ([its README](frontend/README.md)). |
| `hocuspocus/` | The wiki's collaboration server (Y.js over WebSockets), which also applies an agent's edits to a document. |
| `deploy/` | `nginx` (the `logos-nginx` front: TLS, WebSockets, a self-signed certificate when none is given), `helm` (the chart), `keycloak` (a dev identity provider for single sign-on), `kind` (a local cluster for the chart). |
| `docs/` | Design notes and feature specs of core's own (the wiki, its import and export, OIDC, checklists, the timeline). |
| `local-outline/` | An Outline instance of its own, kept as a reference for the wiki's import; not part of the stack. |

## Run the dev stack

```bash
cp .env.example .env     # then replace its changeme_ values
make services            # MongoDB, Redis, RabbitMQ, SeaweedFS, Hocuspocus, core (air, hot reload), the frontend (Vite), nginx
```

Open http://localhost:8080 and create the first admin at `/enroll`. Core's
own port, 8002, is published on the host's loopback for modules run there;
modules run as containers join the `logos-core_logos` network. Each module's
repository has a compose file that does. The docs' Contributing → Development
Stack page has the whole setup, and `CLAUDE.md` has the commands (`make test`,
`make gqlgen`, `make swag`, the integration tests) and the rules the code
keeps.

## Images

A push to `main` publishes the image of each part it touches, from GitHub
Actions: `ghcr.io/logoscore/logos-core` (the server, which a frontend change
publishes too: it embeds the HTML export's viewer), `logos-frontend` (nginx serving the built
UI), `logos-hocuspocus`, `logos-nginx` (the front) and the Helm chart
(`ghcr.io/logoscore/charts/logos`). CI (`ci-core.yml`, `ci-frontend.yml`) runs
the tests, the linters and the builds on the same pushes; publishing does not
wait for it.
