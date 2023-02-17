# Releasing the Lumigo Kubernetes Operator

## How to trigger a new release

Push a commit to the `main` branch that increments linearly the version in the [VERSION](./VERSION) file.

## Release process

The [build and release workflow](./.github/workflows/build-test-release.yml) will publish:

1. **Container images:** Build the [`lumigo-kubernetes-operator`](https://gallery.ecr.aws/lumigo/lumigo-kubernetes-operator) and [`lumigo-kubernetes-telemetry-proxy`](https://gallery.ecr.aws/lumigo/lumigo-kubernetes-telemetry-proxy) images, and push them with the new version as tag to the public Amazon ECR repositories.
1. **Helm chart defaults:** The new version is set as chart and app version in the [chart metadata](./deploy/helm/Chart.yaml); the new version is set as default tag in the [chart default values](./deploy/helm/values.yaml).
1. **Git tag and GitHub release:** Create a new GitHub release that has the new Helm chart tarball attached as artifact.
1. **Helm repository:** Update the index of the Helm repository hosted as GitHub Pages in the [`gh-pages` branch](https://github.com/lumigo-io/lumigo-kubernetes-operator/tree/gh-pages) will be automatically updated, pointing at the Helm chart tarball attached to the newly-created release.
