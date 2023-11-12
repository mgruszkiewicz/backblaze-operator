# Backblaze Operator
Simple Backblaze B2 Operator for Kubernetes

## Description
This project started as a test bed for me to learn operator-sdk. As of right now, it supports  
- [x] Creating and deleting B2 bucket  
- [x] Set and update bucket ACL  
- [x] Setting lifecycle policy on bucket  
- [x] Creating and writing application keys to secret  


## Getting Started
You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

### Running on the cluster

**Using Helm** 

You can add repository using command
```
helm repo add isseispace https://ihyoudou.github.io/helm-charts
helm repo update
```
To install a release named `b2operator` run:
```
helm install b2operator isseispace/backblaze-operator \
--set credentials.b2ApplicationId="your-application-id" \
--set credentials.b2ApplicationKey="your-application-key" \
--set credentials.b2Region="us-west-004" \
--namespace b2operator --create-namespace
```
**Using manifests**

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

### Example CRD Usage

```yaml
apiVersion: b2.issei.space/v1alpha1
kind: Bucket
metadata:
  name: my-b2-bucket
spec:
  acl: public
  bucketLifecycle:
    - fileNamePrefix: "/"
      daysFromUploadingToHiding: 2
      daysFromHidingToDeleting: 3
    - fileNamePrefix: "logs"
      daysFromUploadingToHiding: 5
      daysFromHidingToDeleting: 7
  writeConnectionSecretToRef:
    name: my-bucket-credentials
    namespace: default
```

## Contributing
Any contribution, tips and tricks are highly apperated

### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/).

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/),
which provide a reconcile function responsible for synchronizing resources until the desired state is reached on the cluster.

### Test It Out
1. Install the CRDs into the cluster:

```sh
make install
```

2. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
make run
```

**NOTE:** You can also run this in one step by running: `make install run`

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

