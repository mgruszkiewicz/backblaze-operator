package controller

import (
	backblaze "github.com/mgruszkiewicz/go-backblaze"
)

// fakeB2 is a hand-rolled fake of the B2Client interface. Its zero value
// reproduces the go-backblaze contract for a nonexistent bucket: Bucket()
// returns (nil, nil) — the exact case that used to panic the key controller.
type fakeB2 struct {
	bucket    *backblaze.Bucket
	bucketErr error

	createBucketResp *backblaze.Bucket
	createBucketErr  error

	createKeyResp *backblaze.ApplicationKeyResponse
	createKeyErr  error

	deleteKeyErr    error
	deletedKeyIds   []string
	createdBuckets  []string
	createdKeyNames []string
}

var _ B2Client = (*fakeB2)(nil)

func (f *fakeB2) Bucket(bucketName string) (*backblaze.Bucket, error) {
	return f.bucket, f.bucketErr
}

func (f *fakeB2) CreateBucketWithInfo(bucketName string, bucketType backblaze.BucketType, bucketInfo map[string]string, lifecycleRules []backblaze.LifecycleRule) (*backblaze.Bucket, error) {
	if f.createBucketErr == nil && f.createBucketResp != nil {
		f.createdBuckets = append(f.createdBuckets, bucketName)
	}
	return f.createBucketResp, f.createBucketErr
}

func (f *fakeB2) CreateApplicationKey(keyDetails *backblaze.CreateKeyRequest) (*backblaze.ApplicationKeyResponse, error) {
	if f.createKeyErr == nil && f.createKeyResp != nil {
		f.createdKeyNames = append(f.createdKeyNames, keyDetails.KeyName)
	}
	return f.createKeyResp, f.createKeyErr
}

func (f *fakeB2) DeleteApplicationKey(applicationKeyID string) (*backblaze.ApplicationKeyResponse, error) {
	if f.deleteKeyErr == nil {
		f.deletedKeyIds = append(f.deletedKeyIds, applicationKeyID)
	}
	return nil, f.deleteKeyErr
}
