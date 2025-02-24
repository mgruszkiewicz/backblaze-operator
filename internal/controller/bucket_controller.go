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

package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	b2v1alpha2 "github.com/mgruszkiewicz/backblaze-operator/api/v1alpha2"
	"github.com/mgruszkiewicz/go-backblaze"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const bucketFinalizer = "bucket.b2.issei.space/finalizer"

// BucketReconciler reconciles a Bucket object
type BucketReconciler struct {
	client.Client
	Log       logr.Logger
	Scheme    *runtime.Scheme
	Backblaze *backblaze.B2
}

//+kubebuilder:rbac:groups=b2.issei.space,resources=buckets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=b2.issei.space,resources=buckets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=b2.issei.space,resources=buckets/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;create;update;patch;delete;watch

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

	l := log.FromContext(ctx)

	// Reconciling current api version
	bucket := &b2v1alpha2.Bucket{}

	if err := r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, bucket); err != nil {
		if errors.IsNotFound(err) {
			// object not found, could have been deleted after
			// reconcile request, hence don't requeue
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	if !controllerutil.ContainsFinalizer(bucket, bucketFinalizer) {
		l.Info("Adding Finalizer")
		controllerutil.AddFinalizer(bucket, bucketFinalizer)
		return ctrl.Result{}, r.Update(ctx, bucket)
	}

	if !bucket.DeletionTimestamp.IsZero() {
		l.Info("Bucket is being deleted")
		return r.reconcileDelete(ctx, bucket)
	}

	return r.reconcileCreate(ctx, bucket)
}

func (r *BucketReconciler) reconcileCreate(ctx context.Context, bucket *b2v1alpha2.Bucket) (ctrl.Result, error) {
	// Create or update the bucket
	if err := r.createOrUpdateBucket(ctx, bucket); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *BucketReconciler) createOrUpdateBucket(ctx context.Context, bucket *b2v1alpha2.Bucket) error {
	l := log.FromContext(ctx)
	// Check if the bucket exists
	if err := r.Get(ctx, types.NamespacedName{Name: bucket.Name, Namespace: bucket.Namespace}, bucket); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("unable to fetch Bucket: %v", err)
		}
	}

	// If bucket was not already reconciled (most likely new CR)
	if !bucket.Status.Reconciled {
		// Creating new bucket

		// Define bucket ACL
		bucket_acl := backblaze.AllPrivate
		switch bucket.Spec.AtProvider.Acl {
		case "private":
			bucket_acl = backblaze.AllPrivate
		case "public":
			bucket_acl = backblaze.AllPublic
		}
		// Checking if bucket doesnt already exist
		bucket_b2_exist, bucket_b2_exist_err := r.Backblaze.Bucket(bucket.Name)

		if bucket_b2_exist_err != nil {
			return fmt.Errorf("unable to fetch Bucket: %v", bucket_b2_exist_err)
		}

		if bucket_b2_exist == nil {
			l.Info("Creating Bucket")

			// Creating Bucket
			bucket_b2, bucket_err := r.Backblaze.CreateBucketWithInfo(bucket.Name, bucket_acl, make(map[string]string), bucket.Spec.AtProvider.BucketLifecycle)

			// TODO: add `no_payment_history` handling in lib
			if bucket_b2 == nil {
				l.Info("unable to create bucket, no_payment_history?")
			}

			if bucket_err != nil {
				return fmt.Errorf("unable to create Bucket: %v", bucket_err)
			}

		} else {
			l.Info("Bucket exist at provider, adopting")
		}
		bucket.Status.AtProvider = bucket.Spec.AtProvider
		bucket.Status.Reconciled = true
		r.Status().Update(ctx, bucket)

		return nil
	} else {
		// Updating bucket

		bucket_at_b2, err := r.Backblaze.Bucket(bucket.Name)
		if err != nil {
			return fmt.Errorf("unable to fetch Bucket: %v", err)
		}

		if bucket.Spec.AtProvider.Acl != bucket.Status.AtProvider.Acl || !StringSlicesEqual(bucket.Spec.AtProvider.BucketLifecycle, bucket.Status.AtProvider.BucketLifecycle) {
			l.Info("Bucket resource exist on cluster, updating state")
			bucket_acl := backblaze.AllPrivate
			switch bucket.Spec.AtProvider.Acl {
			case "private":
				bucket_acl = backblaze.AllPrivate
			case "public":
				bucket_acl = backblaze.AllPublic
			}

			update_err := bucket_at_b2.UpdateAll(bucket_acl, make(map[string]string), bucket.Spec.AtProvider.BucketLifecycle, 0)
			if update_err != nil {
				return fmt.Errorf("unable to update Bucket: %v", update_err)
			} else {
				bucket.Status.AtProvider = bucket.Spec.AtProvider
			}
			r.Status().Update(ctx, bucket)
		}

	}

	return nil
}

func (r *BucketReconciler) reconcileDelete(ctx context.Context, bucket *b2v1alpha2.Bucket) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	l.Info("Removing Bucket")

	bucket_b2, _ := r.Backblaze.Bucket(bucket.Name)
	if bucket_b2 == nil {
		l.Info("Bucket not found")
		// Remove the finalizer and update the object
		controllerutil.RemoveFinalizer(bucket, bucketFinalizer)
		if err := r.Update(ctx, bucket); err != nil {
			return ctrl.Result{}, fmt.Errorf("error removing finalizer: %v", err)
		}
	} else {
		// Deleting bucket
		err := bucket_b2.Delete()
		if err != nil {
			l.Error(err, "error occured while trying to remove bucket (is the bucket empty?)")
		} else {
			// Remove the finalizer and update the object
			controllerutil.RemoveFinalizer(bucket, bucketFinalizer)
			if err := r.Update(ctx, bucket); err != nil {
				return ctrl.Result{}, fmt.Errorf("error removing finalizer: %v", err)
			}
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BucketReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&b2v1alpha2.Bucket{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 2}).
		Complete(r)
}
