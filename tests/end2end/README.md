# End-to-end tests

The goal of these tests is to validate the Lumigo operator's behavior across various Kubernetes flavors.

These tests assume:

1. A valid `kubectl` configuration available in the home directory of the user
2. A Lumigo operator installed and up-and-running into the Kubernetes cluster referenced by the `kubectl` configuration