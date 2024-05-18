# Installation

## Using Helm Chart

[Details in README.md](../README.md)

## Using manifests
Helm is recommended way to install this operator, but if you want, you can also install it using manifests.

0. Clone this repository  

1. Create Master Application Key in Backblaze dashboard, base64 encode these credentials and put them in `config/manager/secret.yaml`
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: credentials
type: Opaque
data:
  B2_REGION: dXMtd2VzdC0wMDQ=  # us-west-004
  B2_APPLICATION_ID: ZXhhbXBsZQ==
  B2_APPLICATION_KEY: ZXhhbXBsZQ==

```
2. Install Instances of Custom Resources:

```sh
kustomize build config/default | kubectl apply -f -
```

### Uninstall CRDs
To delete the CRDs from the cluster:

```sh
kustomize build config/default | kubectl delete -f -
```