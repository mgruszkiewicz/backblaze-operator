# Backblaze Operator
Simple Backblaze B2 Operator for Kubernetes

## Description
This project started as a test bed for me to learn operator-sdk. As of right now, it supports  
- Creating and deleting B2 bucket  
- Set and update bucket ACL  
- Setting lifecycle policy on bucket  
- Creating and writing application keys to secret  


## Getting Started
You’ll need a Kubernetes cluster to run against and Backblaze account.

Create a key on Backblaze B2 to use for operator - you can create Master Application Key or regular Application Key (with permissions to create new keys).
* to create Master Application Key, go to Account -> Application Keys -> Generate New Master Application Key
* to create Application Key, you will need to use [b2 CLI tool](https://www.backblaze.com/docs/cloud-storage-command-line-tools) (and you will need to create master key anyway).  
```
$ b2 key create KEY-NAME writeKeys,deleteKeys,listBuckets,listAllBucketNames,readBuckets,writeBuckets,deleteBuckets
```
The output will be space-seperated `application-id application-key`

### Running on the cluster

**Using Helm** 

To install a release named `b2operator` run:
```bash
helm upgrade --install backblaze-operator backblaze-operator \
  --set credentials.b2ApplicationId="your-application-id" \
  --set credentials.b2ApplicationKey="your-application-key" \
  --set credentials.b2Region="us-west-004" \
  --namespace backblaze-operator --create-namespace \
  --repo https://ihyoudou.github.io/helm-charts
```
Replace `your-application-id`, `your-application-key` and `us-west-004` with details that match your account.  
You can view all available values in [helm-charts repo](https://github.com/ihyoudou/helm-charts/tree/main/charts/backblaze-operator).

### Example CRD Usage

Creating bucket with lifecycle policies on prefix `/` and `/logs`. Lifecycle policies are optional.

```yaml
apiVersion: b2.issei.space/v1alpha2
kind: Bucket
metadata:
  name: my-b2-bucket
spec:
  atProvider:
    acl: private
    bucketLifecycle:
      - fileNamePrefix: "/"
        daysFromUploadingToHiding: 2
        daysFromHidingToDeleting: 3
      - fileNamePrefix: "logs"
        daysFromUploadingToHiding: 5
        daysFromHidingToDeleting: 7
```

Creating keys with default permissions that will have access to bucket `my-b2-bucket` and will be saved to `new-key` secret in namespace `default`
```yaml
apiVersion: b2.issei.space/v1alpha2
kind: Key
metadata:
  name: my-b2-key
spec:
  atProvider:
    bucketName: my-b2-bucket
    capabilities:
      - deleteFiles
      - listAllBucketNames
      - listBuckets
      - listFiles
      - readBucketEncryption
      - readBucketReplications
      - readBuckets
      - readFiles
      - shareFiles
      - writeBucketEncryption
      - writeBucketReplications
      - writeFiles
  writeConnectionSecretToRef:
      name: new-key
      namespace: default
```
The secret will contain `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `bucketName`, `endpoint`, `keyName`.

### Learn more
You can find troubleshooting guides and other usefull information in [docs directory](docs/)

### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/).

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/),
which provide a reconcile function responsible for synchronizing resources until the desired state is reached on the cluster.

## License

Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

