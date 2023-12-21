/*
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
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type WriteConnectionSecretToRef struct {
	// Set name of secret where credentials should be set
	Name string `json:"name,omitempty"`
	// Set namespace of secret where credentials should be set
	Namespace string `json:"namespace,omitempty"`
}

// KeySpec defines the desired state of Key
type SpecAtProvider struct {
	// List of capabilities that key should have. Available options: "listKeys", "writeKeys", "deleteKeys", "listAllBucketNames", "listBuckets", "readBuckets", "writeBuckets", "deleteBuckets", "readBucketRetentions", "writeBucketRetentions", "readBucketEncryption", "writeBucketEncryption", "listFiles", "readFiles", "shareFiles", "writeFiles", "deleteFiles", "readFileLegalHolds", "writeFileLegalHolds", "readFileRetentions", "writeFileRetentions", "bypassGovernance"
	Capabilities []string `json:"capabilities,omitempty"`
	// When provided, the key will expire after the given number of seconds, and will have expirationTimestamp set. Value must be a positive integer, and must be less than 1000 days (in seconds).
	// +kubebuilder:validation:Type=integer
	// +kubebuilder:validation:Maximum:=86400000
	ValidDurationInSeconds int `json:"validDurationInSeconds,omitempty"`
	// Name of bucket to which key should have access. Leave empty to allow access to all buckets on account.
	BucketName string `json:"bucketName,omitempty"`
	// Optional: Instead of bucket name, you can define directly a bucket ID. Leave empty to allow access to all buckets on account.
	BucketId string `json:"bucketId,omitempty"`
	// When present, restricts access to files whose names start with the prefix. Note: you need to setup bucketName or bucketId when setting this.
	NamePrefix string `json:"namePrefix,omitempty"`
}

// KeySpec defines the desired state of Key
type KeySpec struct {
	// Define configuration at provider (https://www.backblaze.com/apidocs/b2-create-key)
	AtProvider SpecAtProvider `json:"atProvider,omitempty"`
	//+kubebuilder:validation:Optional
	// Set where operator should save connection credentials.
	WriteConnectionSecretToRef WriteConnectionSecretToRef `json:"writeConnectionSecretToRef,omitempty"`
}

// KeyStatus defines the observed state of Key
type KeyStatus struct {
	// Current configuration at provider
	AtProvider SpecAtProvider `json:"atProvider,omitempty"`
	//+kubebuilder:default=false
	// Status for resource, if it was reconciled by operator
	Reconciled bool `json:"reconciled,omitempty"`
	//+kubebuilder:default=false
	// Internal status for operator: it will be set to true if the key will need to be recreated
	ToRecreate bool `json:"ToRecreate,omitempty"`
	// Internal status for operator: application key ID to perform some operations
	KeyId string `json:"keyId,omitempty"`
	// Internal status for operator: target bucket ID
	BucketId string `json:"bucketId,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Bucket",type="string",JSONPath=`.spec.atProvider.bucketName`
//+kubebuilder:printcolumn:name="Reconciled",type="boolean",JSONPath=`.status.reconciled`
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Key is the Schema for the keys API
type Key struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KeySpec   `json:"spec,omitempty"`
	Status KeyStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// KeyList contains a list of Key
type KeyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Key `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Key{}, &KeyList{})
}
