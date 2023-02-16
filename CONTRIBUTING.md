# Contributing

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

Deploy the Lumigo operator with:

```sh
CONTROLLER_IMG=host.docker.internal:5000/controller:latest make docker-build docker-push
helm install lumigo deploy/helm --namespace lumigo-system --create-namespace --set "controllerManager.manager.image.repository=host.docker.internal:5000/controller" --set "controllerManager.manager.image.tag=latest"
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

## Helm chart

The first version of the [Helm chart](./deploy/helm/) has been generated with [helmify](https://github.com/arttor/helmify) by running `kustomize build config/default | helmify deploy/helm`.
However, manual edits and corrections were necessary, and running `helmify` again would override them.
The Helm website has a [handy guide to the basics](https://helm.sh/docs/chart_template_guide/) of Helm Chart templating.

## Change version of the OpenTelemetry Collector Contrib to be used as telemetry-proxy

The [telemetryproxy/VERSION.otelcontibcol](./telemetryproxy/VERSION.otelcontibcol) file contains the _release name_ of the OpenTelemetry Collector Contrib to be used from the [lumigo-io/opentelemetry-collector-contrib](https://github.com/lumigo-io/opentelemetry-collector-contrib/releases) repository.
