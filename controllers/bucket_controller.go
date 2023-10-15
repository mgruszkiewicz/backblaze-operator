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

package controllers

import (
	"context"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	b2v1alpha1 "github.com/ihyoudou/backblaze-operator/api/v1alpha1"
	"gopkg.in/kothar/go-backblaze.v0"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const bucketFinalizer = "bucket.b2.issei.space/finalizer"

// BucketReconciler reconciles a Bucket object
type BucketReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=b2.issei.space,resources=buckets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=b2.issei.space,resources=buckets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=b2.issei.space,resources=buckets/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Bucket object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *BucketReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	log := r.Log.WithValues("bucket", req.NamespacedName)
	bucket := &b2v1alpha1.Bucket{}

	if err := r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, bucket); err != nil {
		if errors.IsNotFound(err) {
			// object not found, could have been deleted after
			// reconcile request, hence don't requeue
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	if !controllerutil.ContainsFinalizer(bucket, bucketFinalizer) {
		log.Info("Adding Finalizer")
		controllerutil.AddFinalizer(bucket, bucketFinalizer)
		return ctrl.Result{}, r.Update(ctx, bucket)
	}

	if !bucket.DeletionTimestamp.IsZero() {
		log.Info("Bucket is being deleted")
		return r.reconcileDelete(ctx, bucket)
	}
	r.reconcileCreate(ctx, bucket)

	return ctrl.Result{}, nil
}

func (r *BucketReconciler) reconcileCreate(ctx context.Context, bucket *b2v1alpha1.Bucket) (ctrl.Result, error) {
	// Create or update the bucket
	if err := r.createOrUpdateBucket(ctx, bucket); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *BucketReconciler) createBucketSecret(bucket *b2v1alpha1.Bucket) *corev1.Secret {

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bucket.Spec.BucketWriteConnectionSecretToRef.Name,
			Namespace: bucket.Spec.BucketWriteConnectionSecretToRef.Namespace,
		},
		Data: map[string][]byte{"bucketName": []byte(bucket.Name), "endpoint": []byte(fmt.Sprintf("s3.%s.backblazeb2.com", string(os.Getenv("B2_REGION"))))},
	}
}

func (r *BucketReconciler) createOrUpdateBucket(ctx context.Context, bucket *b2v1alpha1.Bucket) error {
	log := r.Log.WithValues("bucket", bucket.Namespace)
	// Check if the bucket exists
	if err := r.Get(ctx, types.NamespacedName{Name: bucket.Name, Namespace: bucket.Namespace}, bucket); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("Unable to fetch Bucket: %v", err)
		}
	}

	// Checking if backblaze secrets are set
	if os.Getenv("B2_APPLICATION_ID") == "" || os.Getenv("B2_APPLICATION_KEY") == "" {
		log.Info("B2_APPLICATION_ID or B2_APPLICATION_KEY not set")
	}
	// Initializing backblaze client
	b2, _ := backblaze.NewB2(backblaze.Credentials{
		AccountID:      os.Getenv("B2_APPLICATION_ID"),
		ApplicationKey: os.Getenv("B2_APPLICATION_KEY"),
	})

	// If bucket was not already reconciled (most likely new CR)
	if !bucket.Status.Reconciled {
		// Creating new bucket

		// Define bucket ACL
		bucket_acl := backblaze.AllPrivate
		switch bucket.Spec.Acl {
		case "private":
			bucket_acl = backblaze.AllPrivate
		case "public":
			bucket_acl = backblaze.AllPublic
		}
		// Checking if bucket doesnt already exist
		bucket_b2_exist, bucket_b2_exist_err := b2.Bucket(bucket.Name)

		if bucket_b2_exist_err != nil {
			return fmt.Errorf("Unable to fetch Bucket: %v", bucket_b2_exist_err)
		}

		if bucket_b2_exist == nil {
			log.Info("Creating Bucket")
			// Creating Bucket
			bucket_b2, bucket_err := b2.CreateBucketWithInfo(bucket.Name, bucket_acl, make(map[string]string), bucket.Spec.BucketLifecycle)

			if bucket_err != nil {
				return fmt.Errorf("Unable to create Bucket: %v", bucket_err)
			}

			fmt.Println(string(bucket_b2.BucketType))
		} else {
			log.Info("Bucket exist at provider, adopting")
		}
		bucket.Status.AtProvider.BucketLifecycle = bucket.Spec.BucketLifecycle
		bucket.Status.AtProvider.Acl = bucket.Spec.Acl
		bucket.Status.Reconciled = true
		r.Status().Update(ctx, bucket)

		// Creating secret with bucket details
		secret := r.createBucketSecret(bucket)
		err := r.Create(ctx, secret)
		if err != nil {
			log.Error(err, "Failed to create new Secret", "Secret.Namespace", secret.Namespace, "Deployment.Name", secret.Name)
		}

		return nil
	} else {
		// Updating bucket
		log.Info("Bucket resource exist on cluster, updating state")

		bucket_at_b2, err := b2.Bucket(bucket.Name)
		if err != nil {
			return fmt.Errorf("Unable to fetch Bucket: %v", err)
		}

		if bucket.Spec.Acl != bucket.Status.AtProvider.Acl || !StringSlicesEqual(bucket.Spec.BucketLifecycle, bucket.Status.AtProvider.BucketLifecycle) {
			bucket_acl := backblaze.AllPrivate
			switch bucket.Spec.Acl {
			case "private":
				bucket_acl = backblaze.AllPrivate
			case "public":
				bucket_acl = backblaze.AllPublic
			}

			update_err := bucket_at_b2.UpdateAll(bucket_acl, make(map[string]string), bucket.Spec.BucketLifecycle, 0)
			if update_err != nil {
				return fmt.Errorf("Unable to update Bucket: %v", update_err)
			} else {
				bucket.Status.AtProvider = bucket.Spec
			}
			r.Status().Update(ctx, bucket)
		}

	}

	return nil
}

func (r *BucketReconciler) reconcileDelete(ctx context.Context, bucket *b2v1alpha1.Bucket) (ctrl.Result, error) {
	log := r.Log.WithValues("bucket", bucket.Namespace)
	log.Info("Removing Bucket")
	// Checking if backblaze secrets are set
	if os.Getenv("B2_APPLICATION_ID") == "" || os.Getenv("B2_APPLICATION_KEY") == "" {
		log.Info("B2_APPLICATION_ID or B2_APPLICATION_KEY not set")
	}
	// Initializing backblaze client
	b2, _ := backblaze.NewB2(backblaze.Credentials{
		AccountID:      os.Getenv("B2_APPLICATION_ID"),
		ApplicationKey: os.Getenv("B2_APPLICATION_KEY"),
	})

	bucket_b2, _ := b2.Bucket(bucket.Name)
	if bucket_b2 == nil {
		log.Info("Bucket not found")
		// Remove the finalizer and update the object
		controllerutil.RemoveFinalizer(bucket, bucketFinalizer)
		if err := r.Update(ctx, bucket); err != nil {
			return ctrl.Result{}, fmt.Errorf("Error removing finalizer: %v", err)
		}
	} else {
		// Deleting bucket
		err := bucket_b2.Delete()
		if err != nil {
			log.Info("Error occured while trying to remove bucket (is the bucket empty?)")
		} else {
			// Remove secret
			secret := r.createBucketSecret(bucket)
			if err := r.Delete(ctx, secret); err != nil {
				log.Info("Failed to remove secret")
			}

			// Remove the finalizer and update the object
			controllerutil.RemoveFinalizer(bucket, bucketFinalizer)
			if err := r.Update(ctx, bucket); err != nil {
				return ctrl.Result{}, fmt.Errorf("Error removing finalizer: %v", err)
			}
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BucketReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&b2v1alpha1.Bucket{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 2}).
		Complete(r)
}
