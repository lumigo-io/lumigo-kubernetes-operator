# Contributing

## House rules

* Commit history must be linear (enforced via GitHub settings).
* Pull Requests must have their title comforming to the [conventional commits](https://www.conventionalcommits.org/en/v1.0.0/) specification (enforced via a [build workflow](.github/workflows/check-pr-title.yml)).
* PR's are squashed on merge (enforced via GitHub settings).
* The PR's title and description are used as to create the message of the squashed commit that is merged into `main`.
* The [changelog](./CHANGELOG.md) is automatically updated on release based on the commit history.

## Local testing with Minikube

Install [minikube](https://minikube.sigs.k8s.io/docs/start/), [Helm](https://helm.sh/docs/intro/install/), [Go](https://go.dev/doc/install) and a local Docker daemon, e.g., [Docker Desktop](https://www.docker.com/products/docker-desktop/).

Set up your Docker engine to use insecure registries (on Mac OS with Docker Desktop for Mac, the file to edit is `~/.docker/daemon.json`):

```json
{
  ...
  "insecure-registries" : [
    "host.docker.internal:5000"
  ],
  ...
}
```

Start `minikube`:

```sh
minikube start --insecure-registry "host.docker.internal:5000"
```

Start a local Docker registry:

```sh
docker run -d -p 5000:5000 --restart=always --name registry registry:latest
```

Validate that the local registry works by calling its HTTP API:

```sh
$ curl localhost:5000/v2/_catalog -v
*   Trying ::1:5000...
* Connected to localhost (::1) port 5000 (#0)
> GET /v2/_catalog HTTP/1.1
> Host: localhost:5000
> User-Agent: curl/7.77.0
> Accept: */*
> 
* Mark bundle as not supporting multiuse
< HTTP/1.1 200 OK
< Content-Type: application/json; charset=utf-8
< Docker-Distribution-Api-Version: registry/2.0
< X-Content-Type-Options: nosniff
< Date: Fri, 20 Jan 2023 08:10:32 GMT
< Content-Length: 20
< 
{"repositories":[]}
* Connection #0 to host localhost left intact
```

### Deploy with Helm

Deploy the Lumigo Kubernetes operator with:

```sh
make docker-build docker-push
helm upgrade --install lumigo charts/lumigo-operator --namespace lumigo-system --create-namespace
```

To avoid strange issues with Docker caching the wrong images in your test environment, it is usually a better to always build a new image tag:

```sh
export IMG_VERSION=1 # Incremend this every time to try a deploy
make docker-build docker-push
helm upgrade --install lumigo charts/lumigo-operator --namespace lumigo-system --create-namespace --set "controllerManager.manager.image.tag=${IMG_VERSION}" --set "controllerManager.telemetryProxy.image.tag=${IMG_VERSION}"
```

Changing the target Lumigo backend is _not_ supported with Helm (because we do not expect end users ever to have to).

### Deploy with Kustomize

Install [`cert-manager`](https://cert-manager.io/) with:

```sh
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.11.0/cert-manager.yaml
```

Deploy the Lumigo Kubernetes operator with:

```
kubectl create namespace lumigo-system
kubectl apply -k config/default -n lumigo-system
```

Changing the target Lumigo backend can be done with a [`patchStrategicMerge`](https://kubectl.docs.kubernetes.io/references/kustomize/glossary/#patchstrategicmerge):

```sh
echo -n "apiVersion: apps/v1
kind: Deployment
metadata:
  name: lumigo-controller-manager
spec:
  template:
    spec:
      containers:
      - name: telemetry-proxy
        env:
        - name: LUMIGO_ENDPOINT
          value: \"https://my.lumigo.endpoint\" # Replace this!
" > lumigo-endpoint.patch.yaml
kubectl patch --patch-file lumigo-endpoint.patch.yaml --type strategic -n lumigo-system --filename=lumigo-endpoint.patch.yaml
```

### Troubleshooting

#### What listens on port 5000 is not behaving like a registry

If you see the following, it's likely because and Mac OS has [squatted over port 5000 with the AirPlay receiver](https://www.reddit.com/r/webdev/comments/qg8yt9/apple_took_over_port_5000_in_the_latest_macos/):

```sh
docker push host.docker.internal:5000/controller
Using default tag: latest
The push refers to repository [host.docker.internal:5000/controller]
377b701db379: Preparing 
fba4381f2bb7: Preparing 
error parsing HTTP 403 response body: unexpected end of JSON input: ""
```

To fix it, disable AirPlay Receiver in `System Preferences --> Sharing --> AirPlay Receiver`.

#### ErrImagePull for the Operator image

If you see which when checking the controller pod with `kubectl describe pod`:

```
  Warning  Failed     7s (x3 over 49s)   kubelet            Failed to pull image "host.docker.internal:5000/controller:latest": rpc error: code = Unknown desc = Error response from daemon: Get "https://host.docker.internal:5000/v2/": http: server gave HTTP response to HTTPS client
```

Then you likely have not started `minikube` with the `--insecure-registry "10.0.0.0/24,192.168.39.0/24,host.docker.internal"` startup parameter.
You will need to _destroy_ the `minikube` installation, as the [`--insecure-registry` parameter is accepted only on creation](https://minikube.sigs.k8s.io/docs/handbook/registry/#enabling-insecure-registries):

```sh
$ minikube delete
🔥  Deleting "minikube" in docker ...
🔥  Deleting container "minikube" ...
🔥  Removing /Users/mmanciop/.minikube/machines/minikube ...
💀  Removed all traces of the "minikube" cluster.
$ minikube start --insecure-registry "host.docker.internal:5000"
```

#### Various Helm stuff

**upgrade --install fails with "no deployed releases":** running `helm upgrade --install` should precisely avoid these situations, making `helm` behave like an `upsert`, right?
   However, sometimes dirty state left by failed uninstall procedures prevents Helm from being able to upsert, and to fix it, you run:
   ```sh
   kubectl delete secrets --all -n lumigo-system
   ```
   See [How to fix Helm's "Upgrade Failed: has no deployed releases" error ](https://dev.to/mxglt/how-to-fix-helms-upgrade-failed-has-no-deployed-releases-error-5cbn) for more info.

## End-to-end tests with Kind

Install [`kind`](https://kind.sigs.k8s.io/) and run:

```sh
(cd tests/kubernetes-distros/kind; go test)
```

## Helm chart

The first version of the [Helm chart](./deploy/helm/) has been generated with [helmify](https://github.com/arttor/helmify) by running `kustomize build config/default | helmify deploy/helm`.
However, manual edits and corrections were necessary, and running `helmify` again would override them.
The Helm website has a [handy guide to the basics](https://helm.sh/docs/chart_template_guide/) of Helm Chart templating.

## Change version of the OpenTelemetry Collector Contrib to be used as telemetry-proxy

The [telemetryproxy/VERSION.otelcontibcol](./telemetryproxy/VERSION.otelcontibcol) file contains the _release name_ of the OpenTelemetry Collector Contrib to be used from the [lumigo-io/opentelemetry-collector-contrib](https://github.com/lumigo-io/opentelemetry-collector-contrib/releases) repository.
