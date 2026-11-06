# logos Helm chart

Deploys the entire Logos stack — Go API, React frontend, Hocuspocus
collab sidecar, plus MongoDB, Redis, RabbitMQ and SeaweedFS — into a
single Kubernetes namespace. Factories and channels (modules) are
deployed on their own and join through RabbitMQ.

```
ingress (host: logos.example.com)
  ├── /                   → frontend (nginx + SPA)
  ├── /api/               → core (Go REST + GraphQL + SSE)
  ├── /api/v1/ws/wiki/    → hocuspocus (WebSocket, Yjs collab)
  └── /swagger/           → core (Swagger UI)

cluster-internal
  ├── core ←→ mongodb, redis, rabbitmq, seaweedfs-s3
  ├── modules (factories, channels; not in this chart) ←→ rabbitmq
  ├── hocuspocus ←→ mongodb (Y.Doc snapshots)
  └── core → hocuspocus:1235 (disconnect / markdown-to-yjs)
     hocuspocus → core:8002 (onConnect/onDisconnect webhooks)
```

## Requirements

- Kubernetes 1.26+
- An ingress controller — defaults assume **ingress-nginx**
- A default `StorageClass`, or one set explicitly via `global.storageClass`
- Helm 3.14+

## Quick start

```sh
# Pull subchart dependencies into ./charts/
helm dependency build ./deploy/helm/logos

# Install with inline secrets (dev only)
helm install logos ./deploy/helm/logos \
  --namespace logos --create-namespace \
  --set ingress.host=logos.lab.local \
  --set secrets.values.jwtSecretKey="$(openssl rand -hex 32)" \
  --set secrets.values.mongoPassword="$(openssl rand -hex 24)" \
  --set secrets.values.redisPassword="$(openssl rand -hex 24)" \
  --set secrets.values.rabbitmqPassword="$(openssl rand -hex 24)" \
  --set secrets.values.rabbitmqErlangCookie="$(openssl rand -hex 32)" \
  --set secrets.values.seaweedfsS3SecretKey="$(openssl rand -hex 24)" \
  --set secrets.values.hocuspocusTicketSecret="$(openssl rand -hex 32)" \
  --set secrets.values.hocuspocusWebhookSecret="$(openssl rand -hex 32)"

# Then open the enrollment page in a browser:
#   http://logos.lab.local/enroll
# Submit the form to create the first admin. The endpoint locks itself
# after the first successful enrollment.
```

For local kind testing, see [`README.kind.md`](./README.kind.md) and
the kind-tuned overrides at [`values-kind.yaml`](./values-kind.yaml).
For production, use the example file at
[`values-production.example.yaml`](./values-production.example.yaml) and
manage secrets through `secrets.existingSecret`.

## Architecture decisions baked into the chart

### Single host, path-based routing
The frontend's `VITE_API_URL` is the relative path `/api/v1`, so the
SPA, REST/GraphQL, and the Yjs WebSocket are all served from the same
origin. The Ingress routes by path. WebSocket upgrades flow through
`/api/v1/ws/wiki/` to the hocuspocus Service.

### Minion transfers get Ingresses of their own
ingress-nginx annotations apply to a whole Ingress, and two paths need
what the rest of `/api/` must not have (`ingress.minionTransfers`):

| Ingress | Path | Body | Buffering | Timeouts |
|---|---|---|---|---|
| `<release>-minion-files` | `POST /api/v1/minion-files` (task files for uploads) | `core.config.minionFile.maxSize` + 64 MiB | none: passed to core as it comes | 1 h |
| `<release>-minion-streams` | `/api/v1/minion-streams/` (stream_download, stream_upload) | no limit | none, either way | 1 h |

They carry `ingress.annotations` too (TLS, cert-manager), with
`ingress.minionTransfers.annotations` over the transfer defaults: a longer
`proxy-read-timeout` for very slow channels, say. The longer paths win over
`/api/`. Core keeps a stream in Redis a few chunks at a time, so any core
replica serves any stream.

### MONGO_URI is composed in-pod
`MONGO_URI` is built from `MONGO_HOST` + `MONGO_PORT` (ConfigMap) and
`MONGO_INITDB_ROOT_USERNAME` + `MONGO_INITDB_ROOT_PASSWORD` (Secret).
This keeps the password out of the ConfigMap while still letting
operators override the host for an external Mongo.

### Core runs as a single replica
Core keeps its data in MongoDB and Redis, so any pod could answer any
request, but its event bus (what feeds the web app's live views, the
GraphQL subscriptions) lives in the process. With two replicas, a change
one pod handles never reaches browsers connected to the other, and a
minion connection held open by one pod could not be reached from the
other. The chart sets `core.replicaCount: 1`. A rollout still runs the new
pod beside the old one for a few seconds before the old one stops; that is
harmless. Scaling core out is planned, not supported: see the docs'
Architecture page, "One core instance".

### Operational ports get a Service of their own
A tunnel task's SOCKS proxy listens on one of core's operational ports
(`core.operationalPorts`: `count` ports from `first`, none by default),
raw TCP that an Ingress does not carry. The chart gives core the range
(`OPERATIONAL_PORTS`) and publishes it with a Service of its own,
`<release>-core-operational`, one port each (a Service takes no ranges),
a LoadBalancer by default. Core's SOCKS takes no credentials: whoever
reaches a port uses its minion, so keep the Service to the operators'
networks (`service.loadBalancerSourceRanges`, or the network in front).
Set `address` to the host they reach it at, which the web app shows.

### Hocuspocus runs as a single replica
Hocuspocus holds Y.Doc state in process memory. Two replicas without
sticky sessions cause split-brain editing. The chart pins
`replicaCount: 1` and uses `Recreate` strategy. To scale, you need a
sticky-session ingress AND a document-affinity routing layer — out of
scope for this chart.

### Bucket bootstrap is an async Job, not a hook
SeaweedFS does not pre-create S3 buckets. The chart ships a Job that
runs `mc mb` against the filer's S3 endpoint to ensure `wiki-images`
and `wiki-files` exist before the first upload. The Job is idempotent
(`--ignore-existing`) and retries while SeaweedFS warms up
(`backoffLimit: 6` plus an in-script wait loop).

It is intentionally **not** a `helm.sh/hook` — wrapping it in a
post-install hook makes `helm install` block for 3–5 minutes on a cold
cluster while SeaweedFS settles, with no helm-side output. Running it
as a regular Job lets `helm install` return immediately; broken
bootstrap surfaces as a `Failed` Job from `kubectl get jobs`. Re-run
after fixing the cause with `kubectl delete job ...-bucket-bootstrap`
followed by `helm upgrade`.

### App Secret is the source of truth
The bitnami MongoDB, Redis and RabbitMQ subcharts read passwords from the
same Secret the app uses (`mongodb-root-password`, `mongodb-passwords`,
`redis-password`, `rabbitmq-password`, `rabbitmq-erlang-cookie` keys are
written in `templates/secret.yaml` to match their conventions). One
rotation point. Keep `rabbitmqErlangCookie` the same across upgrades: the
broker's node is tied to it.

### RabbitMQ is core's control plane
Core does not start without it: module registration, management RPCs and
live module logs go through it, and factories and channels join it to be
seen at all. The bundled broker (bitnami `rabbitmq`, RabbitMQ 4.1) runs one
node with the user `rabbitmq.auth.username` (`logos`) and the
`rabbitmqPassword`; core joins as that user on vhost `/`.

Modules are not in this chart. Run them where they can reach the broker
(`<release>-rabbitmq:5672` in the cluster) with that user, or one you add
in its management UI (port 15672; `kubectl port-forward
svc/<release>-rabbitmq 15672`). Modules outside the cluster need it
exposed: `rabbitmq.service.type: LoadBalancer`, or a broker of your own
(below). Each module's README names the settings it takes
(`RABBITMQ_HOST`, `RABBITMQ_USER`, `RABBITMQ_PASSWORD`, …).

### Bitnami images come from `bitnamilegacy/*`
In August 2025 Bitnami restricted public access to `docker.io/bitnami/*`
([bitnami/charts#35164](https://github.com/bitnami/charts/issues/35164)).
The freely-pullable images now live at `docker.io/bitnamilegacy/*`. Our
`mongodb:`, `redis:` and `rabbitmq:` value blocks override `image.repository` and
`volumePermissions.image.repository` to the legacy mirror. We also set
`global.security.allowInsecureImages: true` because the bitnami chart
NOTES.txt aborts rendering when the image registry isn't on its hardcoded
allowlist. The override is safe — the legacy mirror serves the same
images Bitnami used to publish at the original path.

### Frontend image must be rebuilt from the current `frontend/` tree
`VITE_API_URL` is inlined into the SPA bundle by Vite at build time. The
chart pulls `ghcr.io/logoscore/logos-frontend:main`, so the published
image must be built from a tree that contains both
`frontend/.env.production` (committed; carries `VITE_API_URL=/api/v1`)
and the `frontend/.dockerignore` change that lets `.env.production`
flow into the docker build context. If either is missing at image-build
time, Vite inlines the literal string `undefined` and the SPA hits
`/undefined/status`. The CI workflow at
`.github/workflows/publish-frontend.yml` produces the right image as
long as those two files are committed.

## Bring-your-own infrastructure

To use an existing MongoDB / Redis / RabbitMQ / SeaweedFS:

```yaml
mongodb:
  enabled: false
external:
  mongodb:
    host: mongo.shared.svc.cluster.local
    port: 27017

redis:
  enabled: false
external:
  redis:
    host: redis.shared.svc.cluster.local
    port: 6379

rabbitmq:
  enabled: false
external:
  rabbitmq:
    host: rabbitmq.shared.svc.cluster.local
    port: 5672
    vhost: /
    username: logos      # its password: secrets.values.rabbitmqPassword

seaweedfs:
  enabled: false
external:
  seaweedfs:
    s3Endpoint: http://seaweedfs.shared.svc.cluster.local:8333
```

Credentials still come from `secrets.values` or `secrets.existingSecret`.

## Values reference (high-traffic keys)

| Path | Description |
|------|-------------|
| `ingress.host` | Public hostname; required for browser access |
| `ingress.tls.enabled` | Terminate TLS at the ingress (recommended for production) |
| `ingress.tls.secretName` | Name of the TLS secret in the release namespace |
| `core.image.tag` / `frontend.image.tag` / `hocuspocus.image.tag` | Pin to a specific build; defaults to chart `appVersion` |
| `frontend.replicaCount` | Horizontal scale of the web app's static pods |
| `core.replicaCount` | Keep at 1: [core runs as a single replica](#core-runs-as-a-single-replica) |
| `secrets.existingSecret` | Read all app secrets from a Secret you manage |
| `core.config.oidc.*` | Optional OpenID Connect login (Keycloak etc.). Set `enabled` and `issuerUrl`, register `https://<ingress host>/api/v1/auth/oidc/callback` at the provider, and put the client secret in `secrets.values.oidcClientSecret` (key `oidcClientSecret` in an existing Secret) |
| `core.config.auth.localLoginEnabled` | `false` hides the password form; only valid with `oidc.enabled=true` |
| `core.config.minionFile.maxSize` | Largest file a minion sends core, or core sends a minion (2 GiB; at most 64 GiB); the minion-files Ingress follows it |
| `core.operationalPorts.count` / `.first` / `.address` | [Operational ports](#operational-ports-get-a-service-of-their-own) for tunnel tasks' SOCKS proxies: `count` ports from `first` (none by default), and the host operators reach them at |
| `core.operationalPorts.service.*` | Their Service: `type` (LoadBalancer), `annotations`, `loadBalancerSourceRanges` |
| `ingress.minionTransfers.enabled` / `.annotations` | The minion transfer Ingresses (above), and annotations for them only |
| `secrets.values.*` | Inline secret values (development only) |
| `global.storageClass` | StorageClass applied to all PVCs |
| `mongodb.persistence.size` / `redis.master.persistence.size` / `rabbitmq.persistence.size` / `seaweedfs.*.data.size` | PVC sizing |
| `rabbitmq.auth.username` / `rabbitmq.service.type` | The bundled broker's user (core's and the modules'), and how it is exposed |
| `external.*` | Connection details when subcharts are disabled |

The full surface lives in [`values.yaml`](./values.yaml).

## Troubleshooting

```sh
# Render templates without applying
helm template logos ./deploy/helm/logos -f my-values.yaml | less

# Chart lint
helm lint ./deploy/helm/logos

# Inspect what was deployed
kubectl -n logos get all,ingress,configmap,secret,pvc,job

# Pod logs
kubectl -n logos logs -l app.kubernetes.io/component=core --tail=200
kubectl -n logos logs -l app.kubernetes.io/component=hocuspocus --tail=200

# If the bucket bootstrap Job failed, re-run it
kubectl -n logos delete job logos-bucket-bootstrap
helm upgrade logos ./deploy/helm/logos -f my-values.yaml
```

### "MONGO_URI is not set" on core startup
The chart composes `MONGO_URI` from envvars, so this means the Pod could
not resolve `MONGO_HOST`, or the Secret keys are missing. Check:

```sh
kubectl -n logos get cm,secret
kubectl -n logos describe pod -l app.kubernetes.io/component=core
```

### A large upload fails with 413, or a stream stops at a fixed size
The transfer Ingresses are off, or another layer caps the body. Confirm
`<release>-minion-files` and `<release>-minion-streams` exist
(`ingress.minionTransfers.enabled`), and that a load balancer in front of
the controller allows bodies that large and connections that long.

### Hocuspocus disconnects every few seconds
Almost always an ingress configuration issue. Confirm:
- `nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"` is on the Ingress
- The `/api/v1/ws/wiki/` path matches BEFORE `/api/`
- `ingressClassName` matches your controller

## Updating subchart pins

```sh
helm dependency update ./deploy/helm/logos
```

Then commit the regenerated `Chart.lock`.
