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
	"log"
	"os"

	backblaze "gopkg.in/kothar/go-backblaze.v0"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/go-logr/logr"
	b2v1alpha1 "github.com/ihyoudou/backblaze-operator/api/v1alpha1"
)

// BucketReconciler reconciles a Bucket object
type BucketReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=b2.issei.space,resources=buckets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=b2.issei.space,resources=buckets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=b2.issei.space,resources=buckets/finalizers,verbs=update

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
	// Checking if backblaze secrets are set
	if os.Getenv("B2_APPLICATION_ID") == "" || os.Getenv("B2_APPLICATION_KEY") == "" {
		log.Fatal("B2_APPLICATION_ID or B2_APPLICATION_KEY not set")
	}
	// Initializing backblaze client
	b2, _ := backblaze.NewB2(backblaze.Credentials{
		AccountID:      os.Getenv("B2_APPLICATION_ID"),
		ApplicationKey: os.Getenv("B2_APPLICATION_KEY"),
	})

	log := r.Log.WithValues("bucket", req.NamespacedName)
	bucket := &b2v1alpha1.Bucket{}
	r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, bucket)

	if bucket.Status.Reconciled {
		log.Info("bucket was reconciled")
	} else {
		log.Info("bucket was NOT reconciled")

		// Define bucket ACL
		bucket_acl := backblaze.AllPrivate
		switch bucket.Spec.Acl {
		case "private":
			bucket_acl = backblaze.AllPrivate
		case "public":
			bucket_acl = backblaze.AllPublic
		}
		// Creating Bucket
		bucket_b2, bucket_err := b2.CreateBucket(bucket.Name, bucket_acl)

		if bucket_err != nil {
			log.Info("error occured")
			fmt.Println(bucket_err)
		} else {
			bucket.Status.AtProvider = true
		}

		fmt.Println(string(bucket_b2.BucketType))

		bucket.Status.Reconciled = true
		r.Status().Update(ctx, bucket)
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
