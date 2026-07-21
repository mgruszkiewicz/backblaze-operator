package controller

import (
	"github.com/mgruszkiewicz/go-backblaze"
)

// ConditionTypeReady is the condition type used to report whether a resource
// has been successfully reconciled at the provider and why not otherwise.
const ConditionTypeReady = "Ready"

// Reasons used in the Ready condition.
const (
	ReasonReconciled          = "Reconciled"
	ReasonBucketNotFound      = "BucketNotFound"
	ReasonProviderError       = "ProviderError"
	ReasonKeyCreationFailed   = "KeyCreationFailed"
	ReasonBucketCreateFailed  = "BucketCreationFailed"
	ReasonDuplicateBucketName = "DuplicateBucketName"
	ReasonBucketUpdateFailed  = "BucketUpdateFailed"
)

// B2Client is the subset of the go-backblaze API used by the reconcilers.
// *backblaze.B2 satisfies it; tests provide a fake.
type B2Client interface {
	Bucket(bucketName string) (*backblaze.Bucket, error)
	CreateBucketWithInfo(bucketName string, bucketType backblaze.BucketType, bucketInfo map[string]string, lifecycleRules []backblaze.LifecycleRule) (*backblaze.Bucket, error)
	CreateApplicationKey(keyDetails *backblaze.CreateKeyRequest) (*backblaze.ApplicationKeyResponse, error)
	DeleteApplicationKey(applicationKeyID string) (*backblaze.ApplicationKeyResponse, error)
}

var _ B2Client = (*backblaze.B2)(nil)
