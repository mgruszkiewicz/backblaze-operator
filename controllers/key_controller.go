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
	"reflect"

	"github.com/go-logr/logr"
	b2v1alpha1 "github.com/ihyoudou/backblaze-operator/api/v1alpha1"
	"github.com/ihyoudou/go-backblaze"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const keyFinalizer = "key.b2.issei.space/finalizer"

// KeyReconciler reconciles a Key object
type KeyReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=b2.issei.space,resources=keys,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=b2.issei.space,resources=keys/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=b2.issei.space,resources=keys/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *KeyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("key", req.NamespacedName)
	key := &b2v1alpha1.Key{}

	if err := r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, key); err != nil {
		if errors.IsNotFound(err) {
			// object not found, could have been deleted after
			// reconcile request, hence don't requeue
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	if !controllerutil.ContainsFinalizer(key, keyFinalizer) {
		log.Info("Adding Finalizer")
		controllerutil.AddFinalizer(key, keyFinalizer)
		return ctrl.Result{}, r.Update(ctx, key)
	}

	if !key.DeletionTimestamp.IsZero() {
		log.Info("Key is being deleted")
		return r.reconcileDelete(ctx, key)
	}
	r.reconcileCreate(ctx, key)

	return ctrl.Result{}, nil
}

func (r *KeyReconciler) reconcileCreate(ctx context.Context, key *b2v1alpha1.Key) (ctrl.Result, error) {
	// Create or update the key
	if err := r.createOrUpdateKey(ctx, key); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *KeyReconciler) createKeySecret(key *b2v1alpha1.Key, appkey *backblaze.ApplicationKeyResponse) *corev1.Secret {

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Spec.WriteConnectionSecretToRef.Name,
			Namespace: key.Spec.WriteConnectionSecretToRef.Namespace,
		},
		Data: map[string][]byte{
			"bucketName": []byte(key.Spec.BucketName),
			"endpoint":   []byte(fmt.Sprintf("s3.%s.backblazeb2.com", string(os.Getenv("B2_REGION")))),
			"keyName":    []byte(appkey.KeyName),
			// AWS S3 compatibile variables
			"AWS_ACCESS_KEY_ID":     []byte(appkey.ApplicationKeyId),
			"AWS_SECRET_ACCESS_KEY": []byte(appkey.ApplicationKey),
		},
	}
}

func (r *KeyReconciler) createOrUpdateKey(ctx context.Context, key *b2v1alpha1.Key) error {
	log := r.Log.WithValues("key", key.Namespace)
	log.Info("create or update key")

	// Check if the key exists
	if err := r.Get(ctx, types.NamespacedName{Name: key.Name, Namespace: key.Namespace}, key); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("unable to fetch Key: %v", err)
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

	// If key was not already reconciled (most likely new CR)
	if !key.Status.Reconciled || key.Status.ToRecreate {
		log.Info("Key is not reconciled")
		var b2_bucket_id string

		if key.Spec.BucketName != "" {
			bucket_b2, bucket_b2_err := b2.Bucket(key.Spec.BucketName)
			if bucket_b2_err != nil {
				log.Error(bucket_b2_err, "Failed to find bucket at provider")
			}
			b2_bucket_id = bucket_b2.ID
		} else {
			b2_bucket_id = key.Spec.BucketId
		}

		// Create application key
		applicationKeyCreate, err := b2.CreateApplicationKey(&backblaze.CreateKeyRequest{
			KeyName:      key.Name,
			Capabilities: key.Spec.Capabilities,
			BucketId:     b2_bucket_id,
		})
		if err != nil {
			log.Error(err, "Unable to create application key at provider")
		}

		if applicationKeyCreate.ApplicationKeyId != "" {
			log.Info("Got application key id from provider, creating secret")
			secret := r.createKeySecret(key, applicationKeyCreate)
			if err := r.Create(ctx, secret); err != nil {
				log.Error(err, "Failed to create new Secret", "Secret.Namespace", secret.Namespace, "Deployment.Name", secret.Name)
			}
		}

		// Saving reconcilation and provider config
		key.Status.Reconciled = true
		key.Status.ToRecreate = false
		key.Status.AtProvider = key.Spec
		key.Status.KeyId = applicationKeyCreate.ApplicationKeyId
		r.Status().Update(ctx, key)
	} else {
		// reconciling loop
		log.Info("Key is reconciled")
		if !reflect.DeepEqual(key.Spec, key.Status.AtProvider) && !key.Status.ToRecreate {
			log.Info("Key resource exist on cluster, updating state")
			// Updating resource at cluster
			key.Status.Reconciled = false
			key.Status.ToRecreate = true
			r.Status().Update(ctx, key)

			// Deleting key
			_, err := r.reconcileDelete(ctx, key)
			if err != nil {
				log.Error(err, "Failed to delete secret")
			}

			keyerr := r.createOrUpdateKey(ctx, key)
			if keyerr != nil {
				log.Error(keyerr, "Failed to recreate key")
			}

		}
	}

	return nil
}

func (r *KeyReconciler) reconcileDelete(ctx context.Context, key *b2v1alpha1.Key) (ctrl.Result, error) {

	log := r.Log.WithValues("key", key.Namespace)
	log.Info("Removing Key")

	// Retriving existing secret
	existing_secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      key.Spec.WriteConnectionSecretToRef.Name,
		Namespace: key.Spec.WriteConnectionSecretToRef.Namespace},
		existing_secret,
	); err != nil {
		log.Info("Failed to get existing secret at cluster")
	} else {
		// Remove secret
		if err := r.Delete(ctx, existing_secret); err != nil {
			log.Error(err, "Failed to remove secret")
		} else {
			log.Info("Deleted secret at cluster")
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

	// Deleting application key on b2
	_, err := b2.DeleteApplicationKey(key.Status.KeyId)
	if err != nil {
		log.Error(err, "Failed to delete application key")
	} else {
		log.Info("Deleted Application Key at provider")
	}

	// Remove the finalizer and update the object
	controllerutil.RemoveFinalizer(key, keyFinalizer)
	if err := r.Update(ctx, key); err != nil {
		return ctrl.Result{}, fmt.Errorf("Error removing finalizer: %v", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&b2v1alpha1.Key{}).
		Complete(r)
}
